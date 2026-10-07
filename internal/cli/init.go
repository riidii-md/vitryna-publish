package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/gitcmd"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
	"github.com/riidii-md/vitryna-publish/internal/scaffold"
)

type pathFlags []string

func (p *pathFlags) String() string         { return strings.Join(*p, ",") }
func (p *pathFlags) Set(value string) error { *p = append(*p, value); return nil }

func initCommand(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 || args[0] != "github" {
		fmt.Fprintln(stderr, "vitryna: init requires github; use vitryna init github --help")
		return 2
	}
	flags := flag.NewFlagSet("init github", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", ".", "committed source repository")
	source := flags.String("source-repository", "", "source owner/repo (otherwise inferred from GitHub origin)")
	target := flags.String("target", "", "existing public target owner/repo")
	base := flags.String("base", "main", "public base branch")
	promotion := flags.String("promotion", "vitryna/promote", "managed promotion branch")
	trusted := flags.String("trusted-source", "main", "trusted source branch")
	mergeMode := flags.String("merge", "manual", "manual or explicit auto")
	method := flags.String("merge-method", "", "auto-only squash, merge, or rebase")
	inspection := flags.String("protection-inspection", "", "auto-only effective_rulesets or classic")
	verify := flags.String("verify-command", "", "project command; omitted generates a failing placeholder")
	pinPath := flags.String("pins", "", "explicit release pin manifest (required for offline/development generation)")
	release := flags.String("release-version", version, "must match this released binary; development builds require explicit pins")
	offline := flags.Bool("offline", false, "generate without target lookup; publication preflight remains required")
	var public pathFlags
	flags.Var(&public, "public", "exact file or whole subtree to publish (repeatable)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *repository == "" || *target == "" || len(public) == 0 {
		fmt.Fprintln(stderr, "vitryna: init github requires --target and at least one --public; use --help")
		return 2
	}
	fail := func(code string) int { return failure(stderr, "text", &export.Diagnostic{Code: code}) }
	development := version == "dev" || version == "test" || strings.HasSuffix(version, "-dev")
	if *release != version && !development {
		return fail("setup_release_version_mismatch")
	}
	if development && *pinPath == "" {
		return fail("setup_development_pins_required")
	}
	if *offline && *pinPath == "" {
		return fail("setup_offline_pins_required")
	}
	var pins scaffold.ReleasePins
	var err error
	if *pinPath != "" {
		file, openErr := os.Open(*pinPath)
		if openErr != nil {
			return fail("release_pins_unavailable")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return fail("release_pins_unavailable")
		}
		pins, err = scaffold.DecodePins(data, *release)
	} else {
		pins, err = scaffold.FetchPins(ctx, *release)
	}
	if err != nil {
		return failure(stderr, "text", err)
	}
	if *source == "" {
		if err := gitcmd.Capability(ctx); err != nil {
			return failure(stderr, "text", err)
		}
		data, err := gitcmd.Read(ctx, *repository, 8192, "config", "--local", "--get", "remote.origin.url")
		if err != nil {
			return fail("setup_source_identity_required")
		}
		remote := strings.TrimSpace(string(data))
		switch {
		case strings.HasPrefix(remote, "git@github.com:"):
			*source = strings.TrimPrefix(remote, "git@github.com:")
		case strings.HasPrefix(remote, "https://github.com/"):
			*source = strings.TrimPrefix(remote, "https://github.com/")
		case strings.HasPrefix(remote, "ssh://git@github.com/"):
			*source = strings.TrimPrefix(remote, "ssh://git@github.com/")
		default:
			return fail("setup_source_identity_required")
		}
		*source = strings.TrimSuffix(*source, ".git")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(*source) || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(*target) {
		return fail("setup_repository_identity_invalid")
	}
	baseMissing := false
	if !*offline {
		client := githubapi.New("", nil)
		metadata, err := client.Repository(ctx, *target)
		if err != nil {
			return failure(stderr, "text", err)
		}
		if metadata.ID <= 0 || metadata.FullName != *target || metadata.Private == nil || *metadata.Private {
			return fail("target_metadata_invalid")
		}
		if metadata.DefaultBranch == *promotion {
			return fail("promotion_ref_reserved")
		}
		tip, err := client.Ref(ctx, *target, *base)
		if err != nil {
			return failure(stderr, "text", err)
		}
		baseMissing = tip == ""
	}
	options := scaffold.Options{Repository: *repository, SourceRepository: *source, GitHub: config.GitHub{Repository: *target, BaseBranch: *base, PromotionBranch: *promotion, TrustedSourceBranch: *trusted, Merge: config.Merge{Mode: *mergeMode, Method: *method, ProtectionInspection: *inspection}}, PublicPaths: public, VerifyCommand: *verify, Pins: pins}
	result, err := scaffold.Init(ctx, options)
	if err != nil {
		return failure(stderr, "text", err)
	}
	var output strings.Builder
	if result.Existing {
		output.WriteString("Existing generated setup already matches. No files changed.\n")
	} else {
		output.WriteString("Created repository-owned setup:\n")
		for _, path := range result.Files {
			output.WriteString("  " + path + "\n")
		}
	}
	if *offline {
		output.WriteString("Target unchecked (offline). Run publication preflight before enabling the workflow.\n")
	} else if baseMissing {
		output.WriteString("Target base is missing. Initialize it yourself before enabling publication; Vitryna did not create history.\n")
	}
	if strings.TrimSpace(*verify) == "" {
		output.WriteString("No verification command supplied: the generated verify job intentionally fails until configured.\n")
	}
	output.WriteString("Review .vitryna-setup.md for the dedicated App, protections, credentials, and required checks; commit the three files after review.\n")
	return write(stdout, stderr, output.String())
}
