package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/artifact"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
	"github.com/riidii-md/vitryna-publish/internal/ownedpath"
)

func workflow(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "vitryna: workflow requires a subcommand")
		return 2
	}
	if args[0] == "invalidate" || args[0] == "reconcile" || args[0] == "preflight" {
		return control(ctx, args[0], args[1:], stdout, stderr, version)
	}
	if args[0] == "compare" {
		return compareCommand(ctx, args[1:], stdout, stderr)
	}
	if args[0] != "seal" && args[0] != "verify-artifact" {
		fmt.Fprintf(stderr, "vitryna: workflow %q is not implemented in this development build\n", args[0])
		return 1
	}
	flags := flag.NewFlagSet("workflow "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	tree := flags.String("tree", "", "receipt-bound exported tree")
	receiptPath := flags.String("receipt", "", "canonical receipt outside tree")
	receiptDigest := flags.String("receipt-sha256", "", "original receipt SHA-256 captured before project commands (tree verification only)")
	output := flags.String("output", "", "fresh sealed or extracted bundle destination")
	archive := flags.String("archive", "", "downloaded provider ZIP")
	digest := flags.String("sha256", "", "expected raw provider ZIP SHA-256")
	var expected githubapi.ArtifactExpectation
	if args[0] == "verify-artifact" {
		flags.StringVar(&expected.Repository, "source-repository", "", "private artifact repository")
		flags.Int64Var(&expected.RepositoryID, "source-repository-id", 0, "private artifact repository numeric ID")
		flags.Int64Var(&expected.HeadRepositoryID, "artifact-head-repository-id", 0, "event's exact head repository ID (preview may be a fork)")
		flags.Int64Var(&expected.RunID, "run-id", 0, "seal workflow run ID")
		flags.Int64Var(&expected.ArtifactID, "artifact-id", 0, "seal job's immutable artifact ID")
		flags.StringVar(&expected.SourceCommit, "source-commit", "", "seal source commit")
		flags.StringVar(&expected.Digest, "artifact-digest", "", "seal job's raw artifact digest")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	valid := flags.NArg() == 0
	bindingProvided := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "receipt-sha256" {
			bindingProvided = true
		}
	})
	if bindingProvided {
		valid = valid && args[0] == "verify-artifact" && *tree != "" && regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*receiptDigest)
	}
	providerMode := expected.Repository != "" || expected.RepositoryID != 0 || expected.HeadRepositoryID != 0 || expected.RunID != 0 || expected.ArtifactID != 0 || expected.SourceCommit != "" || expected.Digest != ""
	if args[0] == "seal" {
		valid = valid && *tree != "" && *receiptPath != "" && *output != "" && *archive == "" && *digest == ""
	} else {
		if providerMode {
			valid = valid && *tree == "" && *receiptPath == "" && *archive == "" && *digest == "" && *output != "" && regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(expected.Repository) && expected.RepositoryID > 0 && expected.HeadRepositoryID > 0 && expected.RunID > 0 && expected.ArtifactID > 0 && githubapi.ValidOID(expected.SourceCommit) && regexp.MustCompile(`^(sha256:)?[0-9a-f]{64}$`).MatchString(expected.Digest)
		} else {
			valid = valid && ((*tree != "" && *receiptPath != "" && *output == "" && *archive == "" && *digest == "") || (*tree == "" && *receiptPath == "" && *output != "" && *archive != "" && *digest != ""))
		}
	}
	if !valid {
		fmt.Fprintln(stderr, "vitryna: invalid artifact arguments; use workflow "+args[0]+" --help")
		return 2
	}
	var receipt export.Receipt
	var err error
	if args[0] == "seal" {
		err = artifact.Seal(ctx, *tree, *receiptPath, *output)
		if err == nil {
			return write(stdout, stderr, "Sealed tree.tar and receipt.json\n")
		}
	} else if providerMode {
		for name, value := range map[string]string{"GITHUB_REPOSITORY": expected.Repository, "GITHUB_REPOSITORY_ID": strconv.FormatInt(expected.RepositoryID, 10), "GITHUB_RUN_ID": strconv.FormatInt(expected.RunID, 10)} {
			if known := os.Getenv(name); known != "" && known != value {
				return publicationFailure(stderr, "verify-artifact", &export.Diagnostic{Code: "publication_source_identity"})
			}
		}
		token := os.Getenv("VITRYNA_SOURCE_TOKEN")
		if token == "" {
			return publicationFailure(stderr, "verify-artifact", &export.Diagnostic{Code: "publication_credentials_missing"})
		}
		stage, stageErr := ownedpath.TempDir("", "vitryna-preview-download-")
		if stageErr != nil {
			return failure(stderr, "json", &export.Diagnostic{Code: "publication_stage_failed"})
		}
		defer stage.Close()
		zipPath := filepath.Join(stage.Path, "provider.zip")
		err = githubapi.New(token, nil).DownloadArtifact(ctx, expected, zipPath)
		if err == nil {
			receipt, err = artifact.ExtractBound(ctx, zipPath, strings.TrimPrefix(expected.Digest, "sha256:"), *output, expected.SourceCommit, version)
		}
		var diagnostic *export.Diagnostic
		if errors.As(err, &diagnostic) && diagnostic.Code == "artifact_receipt_identity" {
			err = &export.Diagnostic{Code: "publication_receipt_identity"}
		}
		if cleanupErr := stage.Close(); cleanupErr != nil {
			err = &export.Diagnostic{Code: "publication_cleanup_failed"}
		}
	} else if *archive != "" {
		receipt, err = artifact.Extract(ctx, *archive, *digest, *output)
	} else {
		receipt, err = artifact.VerifyBound(ctx, *tree, *receiptPath, *receiptDigest)
	}
	if err != nil {
		return failure(stderr, "json", err)
	}
	return jsonOutput(stdout, stderr, receipt)
}
