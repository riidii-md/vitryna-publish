package artifact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractBoundReceiptIdentityBeforeExposure(t *testing.T) {
	tree, receiptPath, receipt := fixture(t)
	bundle := filepath.Join(t.TempDir(), "seal")
	if err := Seal(context.Background(), tree, receiptPath, bundle); err != nil {
		t.Fatal(err)
	}
	archive, digest := providerZIP(t, bundle, nil)
	for _, scenario := range []string{"original", "wrong source", "wrong version", "missing identity"} {
		t.Run(scenario, func(t *testing.T) {
			source, version := receipt.SourceCommit, receipt.ToolVersion
			switch scenario {
			case "wrong source":
				source = strings.Repeat("f", 40)
			case "wrong version":
				version = "different"
			case "missing identity":
				source = ""
			}
			output := filepath.Join(t.TempDir(), "preview")
			_, err := ExtractBound(context.Background(), archive, digest, output, source, version)
			if scenario == "original" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(output, "tree", "README.md")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("receipt identity mismatch accepted")
				}
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("mismatched preview exposed output")
				}
			}
		})
	}
}
