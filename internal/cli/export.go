package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
)

func exportCommand(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repository", ".", "source repository")
	ref := flags.String("ref", "HEAD", "committed source ref")
	format := flags.String("format", "text", "text or json")
	output := flags.String("output", "", "public tree destination outside source")
	receiptPath := flags.String("receipt", "", "new receipt outside source and output")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *repository == "" || *ref == "" || *output == "" || *receiptPath == "" || *format != "text" && *format != "json" {
		fmt.Fprintln(stderr, "vitryna: export requires --output and --receipt; use export --help")
		return 2
	}
	snapshot, policy, err := committed(ctx, *repository, *ref)
	if err != nil {
		return failure(stderr, *format, err)
	}
	control, err := snapshot.ReadFile(ctx, ".vitryna.yml", config.MaxBytes)
	if err != nil {
		return failure(stderr, *format, err)
	}
	digest := sha256.Sum256(control)
	receipt, err := snapshot.Export(ctx, export.ExportRequest{Policy: policy, ToolVersion: version, ConfigSHA256: hex.EncodeToString(digest[:]), OutputPath: *output, ReceiptPath: *receiptPath})
	if err != nil {
		return failure(stderr, *format, err)
	}
	if *format == "json" {
		return jsonOutput(stdout, stderr, receipt)
	}
	return write(stdout, stderr, fmt.Sprintf("Exported %d files (%d bytes); tree sha256 %s\n", receipt.FileCount, receipt.TotalBytes, receipt.TreeSHA256))
}
