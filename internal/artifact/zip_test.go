package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/riidii-md/vitryna-publish/export"
)

func TestZIPEnvelopeRefusals(t *testing.T) {
	tree, receiptPath, _ := fixture(t)
	bundle := filepath.Join(t.TempDir(), "bundle")
	if err := Seal(context.Background(), tree, receiptPath, bundle); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"traversal", "duplicate", "symlink", "extra", "expanded limit", "receipt limit", "existing destination", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			var buffer bytes.Buffer
			w := zip.NewWriter(&buffer)
			for _, name := range []string{"tree.tar", "receipt.json"} {
				data, err := os.ReadFile(filepath.Join(bundle, name))
				if err != nil {
					t.Fatal(err)
				}
				h := &zip.FileHeader{Name: name, Method: zip.Store}
				if name == "tree.tar" {
					switch scenario {
					case "traversal":
						h.Name = "../tree.tar"
					case "symlink":
						h.SetMode(os.ModeSymlink | 0777)
					case "expanded limit":
						h.UncompressedSize64 = uint64(export.MaxArtifactBytes) + 1
					}
				}
				if name == "receipt.json" && scenario == "receipt limit" {
					h.UncompressedSize64 = uint64(export.MaxReceiptBytes) + 1
				}
				if scenario == "expanded limit" || scenario == "receipt limit" {
					h.CompressedSize64 = uint64(len(data))
					h.Flags = 0
					entry, err := w.CreateRaw(h)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := entry.Write(data); err != nil {
						t.Fatal(err)
					}
				} else {
					entry, err := w.CreateHeader(h)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := entry.Write(data); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario == "duplicate" || scenario == "extra" {
				name := "tree.tar"
				if scenario == "extra" {
					name = "extra"
				}
				if _, err := w.Create(name); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "bad.zip")
			if err := os.WriteFile(archive, buffer.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(buffer.Bytes())
			dest := filepath.Join(t.TempDir(), "result")
			if scenario == "existing destination" {
				if err := os.Mkdir(dest, 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := Extract(ctx, archive, hex.EncodeToString(sum[:]), dest); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
			if scenario == "existing destination" {
				entries, err := os.ReadDir(dest)
				if err != nil || len(entries) != 0 {
					t.Fatal("caller-owned directory changed")
				}
			} else if _, err := os.Lstat(dest); !os.IsNotExist(err) {
				t.Fatal("failed ZIP exposed destination")
			}
		})
	}
}

func TestSealRejectsMutationAndExistingDestination(t *testing.T) {
	tree, receiptPath, _ := fixture(t)
	for _, scenario := range []string{"existing", "inside tree", "mutation"} {
		t.Run(scenario, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "sealed")
			switch scenario {
			case "existing":
				if err := os.Mkdir(dest, 0700); err != nil {
					t.Fatal(err)
				}
			case "inside tree":
				dest = filepath.Join(tree, "sealed")
			case "mutation":
				if err := os.Chmod(filepath.Join(tree, "cmd/run"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := Seal(context.Background(), tree, receiptPath, dest); err == nil {
				t.Fatal("invalid seal accepted")
			}
		})
	}
}
