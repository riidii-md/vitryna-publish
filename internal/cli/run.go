package cli

import (
	"context"
	_ "embed"
	"fmt"
	"io"
)

//go:embed notices.txt
var notices string

const help = `Usage: vitryna <command> [options]

Commands:
  init github  Generate repository-owned publication setup
  check        Validate committed policy and classify tracked paths
  explain      Explain a path's visibility
  export       Export a committed public tree and deterministic receipt
  licenses     Print embedded licenses and dependency notices
  version      Print the version
  help         Show this help

GitHub workflow plumbing: vitryna workflow <invalidate|seal|verify-artifact|compare|reconcile|preflight>.
The GitHub adapter is experimental; hosted acceptance remains required.
`

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, "vitryna:", err)
		return 1
	}
	if len(args) == 0 {
		return write(stdout, stderr, help)
	}
	switch args[0] {
	case "help", "--help", "-h", "version", "--version", "licenses":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "vitryna: unexpected arguments")
			return 2
		}
		if args[0] == "version" || args[0] == "--version" {
			return write(stdout, stderr, "vitryna "+version+"\n")
		}
		if args[0] == "licenses" {
			return write(stdout, stderr, notices)
		}
		return write(stdout, stderr, help)
	case "init", "check", "explain", "export", "workflow":
		if args[0] == "init" {
			return initCommand(ctx, args[1:], stdout, stderr, version)
		}
		if args[0] == "workflow" {
			return workflow(ctx, args[1:], stdout, stderr, version)
		}
		if args[0] == "export" {
			return exportCommand(ctx, args[1:], stdout, stderr, version)
		}
		if args[0] == "check" || args[0] == "explain" {
			return inspect(ctx, args, stdout, stderr)
		}
		fmt.Fprintf(stderr, "vitryna: %s is not implemented in this development build\n", args[0])
		return 1
	default:
		fmt.Fprintf(stderr, "vitryna: unknown command %q; use vitryna help\n", args[0])
		return 2
	}
}

func write(stdout, stderr io.Writer, value string) int {
	if _, err := io.WriteString(stdout, value); err != nil {
		fmt.Fprintln(stderr, "vitryna: cannot write output")
		return 1
	}
	return 0
}
