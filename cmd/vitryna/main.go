package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/riidii-md/vitryna-publish/internal/cli"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, version)
	cancel()
	os.Exit(code)
}
