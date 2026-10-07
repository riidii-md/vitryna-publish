package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/gitcmd"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

func inspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", ".", "source repository")
	ref := flags.String("ref", "HEAD", "committed source ref")
	format := flags.String("format", "text", "text or json")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	wantArgs := 0
	if args[0] == "explain" {
		wantArgs = 1
	}
	if flags.NArg() != wantArgs || *repository == "" || *ref == "" || *format != "text" && *format != "json" {
		fmt.Fprintln(stderr, "vitryna: invalid arguments; use command --help")
		return 2
	}
	snapshot, policy, err := committed(ctx, *repository, *ref)
	if err != nil {
		return failure(stderr, *format, err)
	}
	if args[0] == "check" {
		result, err := snapshot.Check(ctx, policy)
		if err != nil {
			return failure(stderr, *format, err)
		}
		if *format == "json" {
			return jsonOutput(stdout, stderr, result)
		}
		if _, err := fmt.Fprintf(stdout, "Source commit: %s\n", result.SourceCommit); err != nil {
			return failure(stderr, "text", err)
		}
		for _, path := range result.Paths {
			if _, err := fmt.Fprintf(stdout, "%s %q (rule %q, mode %s)\n", path.Visibility, path.Path, path.Rule, path.Mode); err != nil {
				return failure(stderr, "text", err)
			}
		}
		return 0
	}
	result, err := snapshot.Explain(ctx, policy, flags.Arg(0))
	if err != nil {
		return failure(stderr, *format, err)
	}
	if *format == "json" {
		return jsonOutput(stdout, stderr, result)
	}
	return write(stdout, stderr, fmt.Sprintf("%q: %s (rule %q, tracked=%t)\n", result.Path, result.Visibility, result.Rule, result.Tracked))
}

func committed(ctx context.Context, repository, ref string) (*export.Snapshot, export.Policy, error) {
	snapshot, err := export.Open(ctx, repository, ref)
	if err != nil {
		return nil, export.Policy{}, err
	}
	data, err := snapshot.ReadFile(ctx, ".vitryna.yml", config.MaxBytes)
	if err != nil {
		return nil, export.Policy{}, err
	}
	c, err := config.Decode(data)
	if err != nil {
		return nil, export.Policy{}, err
	}
	return snapshot, c.ExportPolicy(), nil
}

func jsonOutput(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(stderr, "vitryna: cannot write output")
		return 1
	}
	return 0
}

func failure(stderr io.Writer, format string, err error) int {
	if format == "json" {
		diagnostic := diagnosticFor(err)
		_ = json.NewEncoder(stderr).Encode(struct {
			Error *export.Diagnostic `json:"error"`
		}{diagnostic})
	} else {
		fmt.Fprintf(stderr, "vitryna: %q\n", err.Error())
	}
	return 1
}

func diagnosticFor(err error) *export.Diagnostic {
	diagnostic := &export.Diagnostic{Code: "runtime_failure"}
	var specific *export.Diagnostic
	var provider *githubapi.Error
	if errors.As(err, &specific) {
		return specific
	}
	if errors.As(err, &provider) {
		diagnostic.Code = provider.Code
	} else if errors.Is(err, gitcmd.ErrSourceRead) {
		diagnostic.Code = "source_git_read_failed"
	} else if errors.Is(err, gitcmd.ErrUnsupportedGit) {
		diagnostic.Code = "source_git_unsupported"
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		diagnostic.Code = "cancelled"
	}
	return diagnostic
}
