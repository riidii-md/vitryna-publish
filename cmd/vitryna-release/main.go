package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/riidii-md/vitryna-publish/internal/releasebuild"
)

func main() {
	version := flag.String("version", "", "explicit public release version, e.g. v0.1.0-rc.1")
	output := flag.String("output", "", "fresh output directory")
	repository := flag.String("repository", ".", "Vitryna source root")
	flag.Parse()
	if flag.NArg() != 0 || *version == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "vitryna-release: requires --version and --output")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	result, err := releasebuild.Build(ctx, *repository, *version, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
}
