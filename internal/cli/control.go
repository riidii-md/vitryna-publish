package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/artifact"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
	"github.com/riidii-md/vitryna-publish/internal/githubpublish"
)

func control(ctx context.Context, command string, args []string, stdout, stderr io.Writer, version string) int {
	flags := flag.NewFlagSet("workflow "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", ".", "credential-free source checkout")
	ref := flags.String("ref", "HEAD", "selected committed source")
	sourceRepository := flags.String("source-repository", "", "rendered source owner/repo")
	sourceCommit := flags.String("source-commit", "", "workflow-selected immutable source commit")
	target := flags.String("target", "", "rendered target owner/repo")
	base := flags.String("base", "", "rendered public base branch")
	promotion := flags.String("promotion", "", "rendered managed promotion branch")
	trusted := flags.String("trusted-source", "", "rendered trusted source branch")
	var repositoryID, runID, artifactID int64
	var artifactDigest string
	if command == "reconcile" {
		flags.Int64Var(&repositoryID, "source-repository-id", 0, "source repository numeric ID")
		flags.Int64Var(&runID, "run-id", 0, "seal workflow run ID")
		flags.Int64Var(&artifactID, "artifact-id", 0, "seal job's immutable artifact ID")
		flags.StringVar(&artifactDigest, "artifact-digest", "", "seal job's raw provider artifact SHA-256")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if command == "reconcile" && (repositoryID <= 0 || runID <= 0 || artifactID <= 0 || !regexp.MustCompile(`^(sha256:)?[0-9a-f]{64}$`).MatchString(artifactDigest)) {
		fmt.Fprintln(stderr, "vitryna: reconcile requires exact seal identity flags; use workflow reconcile --help")
		return 2
	}
	if flags.NArg() != 0 || *repository == "" || *ref == "" || *sourceRepository == "" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(*sourceRepository) || !githubapi.ValidOID(*sourceCommit) || *target == "" || *base == "" || *promotion == "" || *trusted == "" {
		fmt.Fprintln(stderr, "vitryna: publication requires exact rendered identity flags; use workflow "+command+" --help")
		return 2
	}
	fail := func(err error) int { return publicationFailure(stderr, command, err) }
	if known := os.Getenv("GITHUB_REPOSITORY"); known != "" && known != *sourceRepository {
		return fail(&export.Diagnostic{Code: "publication_source_identity"})
	}
	if known := os.Getenv("GITHUB_SHA"); known != "" && known != *sourceCommit {
		return fail(&export.Diagnostic{Code: "publication_source_identity"})
	}
	if command == "reconcile" {
		for name, expected := range map[string]int64{"GITHUB_REPOSITORY_ID": repositoryID, "GITHUB_RUN_ID": runID} {
			if known := os.Getenv(name); known != "" && known != strconv.FormatInt(expected, 10) {
				return fail(&export.Diagnostic{Code: "publication_source_identity"})
			}
		}
	}
	snapshot, err := export.Open(ctx, *repository, *ref)
	if err != nil {
		return fail(err)
	}
	data, err := snapshot.ReadFile(ctx, ".vitryna.yml", config.MaxBytes)
	if err != nil {
		return fail(err)
	}
	cfg, err := config.Decode(data)
	if err != nil {
		return fail(err)
	}
	if cfg.GitHub == nil {
		return fail(&export.Diagnostic{Code: "publication_configuration_missing"})
	}
	g := *cfg.GitHub
	if snapshot.Commit() != *sourceCommit || g.Repository != *target || g.BaseBranch != *base || g.PromotionBranch != *promotion || g.TrustedSourceBranch != *trusted {
		return fail(&export.Diagnostic{Code: "publication_identity_mismatch"})
	}
	sourceToken, clientID, key := os.Getenv("VITRYNA_SOURCE_TOKEN"), os.Getenv("VITRYNA_APP_CLIENT_ID"), os.Getenv("VITRYNA_APP_PRIVATE_KEY")
	if sourceToken == "" || clientID == "" || key == "" {
		return fail(&export.Diagnostic{Code: "publication_credentials_missing"})
	}
	sourceClient := githubapi.New(sourceToken, nil)
	fresh := func(ctx context.Context) error {
		tip, err := sourceClient.Ref(ctx, *sourceRepository, g.TrustedSourceBranch)
		if err != nil {
			return err
		}
		if tip != snapshot.Commit() {
			return &export.Diagnostic{Code: "publication_source_stale"}
		}
		return nil
	}
	if err := fresh(ctx); err != nil {
		return fail(err)
	}
	var tree string
	var receipt export.Receipt
	scope := githubapi.TokenPermissions{}
	if command == "preflight" {
		scope.ReadOnly = true
		scope.ClassicProtection = g.Merge.Mode == "auto" && g.Merge.ProtectionInspection == "classic"
	}
	if command == "reconcile" {
		if _, err := snapshot.Check(ctx, cfg.ExportPolicy()); err != nil {
			return fail(err)
		}
		source, err := sourceClient.Repository(ctx, *sourceRepository)
		if err != nil {
			return fail(err)
		}
		if source.ID != repositoryID || source.FullName != *sourceRepository {
			return fail(&export.Diagnostic{Code: "publication_source_identity"})
		}
		stage, err := os.MkdirTemp("", "vitryna-reconcile-")
		if err != nil {
			return fail(&export.Diagnostic{Code: "publication_stage_failed"})
		}
		owned, err := os.Lstat(stage)
		if err != nil {
			return fail(&export.Diagnostic{Code: "publication_stage_failed"})
		}
		defer func() {
			if current, err := os.Lstat(stage); err == nil && os.SameFile(owned, current) {
				os.RemoveAll(stage)
			}
		}()
		archive := filepath.Join(stage, "provider.zip")
		if err := sourceClient.DownloadArtifact(ctx, githubapi.ArtifactExpectation{Repository: *sourceRepository, RepositoryID: repositoryID, RunID: runID, ArtifactID: artifactID, SourceCommit: *sourceCommit, Digest: artifactDigest}, archive); err != nil {
			return fail(err)
		}
		bundle := filepath.Join(stage, "bundle")
		receipt, err = artifact.Extract(ctx, archive, strings.TrimPrefix(artifactDigest, "sha256:"), bundle)
		if err != nil {
			return fail(err)
		}
		if err := validatePublicationReceipt(receipt, *sourceCommit, data, cfg.ExportPolicy(), version); err != nil {
			return fail(err)
		}
		tree = filepath.Join(bundle, "tree")
		public := githubapi.New("", nil)
		targetMetadata, err := public.Repository(ctx, g.Repository)
		if err != nil {
			return fail(err)
		}
		if targetMetadata.ID <= 0 || targetMetadata.FullName != g.Repository || targetMetadata.Private == nil || *targetMetadata.Private || targetMetadata.DefaultBranch == "" || targetMetadata.DefaultBranch == g.PromotionBranch {
			return fail(&export.Diagnostic{Code: "target_metadata_invalid"})
		}
		baseCommit, err := public.Ref(ctx, g.Repository, g.BaseBranch)
		if err != nil {
			return fail(err)
		}
		if baseCommit == "" {
			return fail(&export.Diagnostic{Code: "public_base_missing"})
		}
		scope.Workflows, err = public.HasWorkflows(ctx, g.Repository, baseCommit)
		if err != nil {
			return fail(err)
		}
		for _, file := range receipt.Files {
			if strings.HasPrefix(file.Path, ".github/workflows/") {
				scope.Workflows = true
			}
		}
		scope.ClassicProtection = g.Merge.Mode == "auto" && g.Merge.ProtectionInspection == "classic"
		if err := fresh(ctx); err != nil {
			return fail(err)
		}
	}
	session, err := githubapi.OpenApp(ctx, clientID, []byte(key), g.Repository, scope, nil)
	if err != nil {
		return fail(err)
	}
	publisher := githubpublish.Publisher{Config: g, API: session.Client, BotID: session.BotID, Fresh: fresh}
	var result githubpublish.Result
	var runErr error
	if command == "reconcile" {
		result, runErr = publisher.Reconcile(ctx, githubpublish.ReconcileRequest{Directory: tree, Receipt: receipt, Remote: "https://github.com/" + g.Repository + ".git", BotLogin: session.BotLogin, Token: session.InstallationToken()})
	} else if command == "preflight" {
		result, runErr = publisher.Preflight(ctx)
	} else {
		result, runErr = publisher.Invalidate(ctx)
	}
	revokeErr := session.Close(ctx)
	if runErr != nil {
		link := "https://github.com/" + g.Repository + "/tree/" + url.PathEscape(g.PromotionBranch)
		if result.PullNumber > 0 {
			link = fmt.Sprintf("https://github.com/%s/pull/%d", g.Repository, result.PullNumber)
		}
		return publicationFailure(stderr, command, runErr, link)
	}
	if revokeErr != nil {
		return fail(revokeErr)
	}
	return jsonOutput(stdout, stderr, result)
}

func validatePublicationReceipt(receipt export.Receipt, commit string, configBytes []byte, policy export.Policy, version string) error {
	digest := sha256.Sum256(configBytes)
	if receipt.SourceCommit != commit || receipt.ConfigSHA256 != hex.EncodeToString(digest[:]) || receipt.PolicySHA256 != policy.Digest() || receipt.ToolVersion != version {
		return &export.Diagnostic{Code: "publication_receipt_identity"}
	}
	return nil
}

func publicationFailure(stderr io.Writer, stage string, err error, candidateURL ...string) int {
	diagnostic := diagnosticFor(err)
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		fmt.Fprintf(stderr, "::error title=Vitryna %s::%s. Publication stopped; inspect the owned candidate and rerun from the current trusted source after correction.\n", stage, diagnostic.Code)
		if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
			file, writeErr := os.OpenFile(summary, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if writeErr == nil {
				fmt.Fprintf(file, "## Vitryna %s failed\n\nCode: `%s`. Publication stopped. Inspect the owned candidate, correct the cause, and rerun the current trusted source. An invalidation failure may leave an older PR armed.\n", stage, diagnostic.Code)
				if len(candidateURL) > 0 {
					fmt.Fprintf(file, "\n[Affected public candidate](%s).\n", candidateURL[0])
				}
				file.Close()
			}
		}
	}
	return failure(stderr, "json", err)
}
