package export_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
)

func ExampleSnapshot_Export() {
	root, err := os.MkdirTemp("", "vitryna-library-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		panic(err)
	}
	configBytes := []byte("version: 1\nexport:\n  public: [README.md]\n  private: [private.txt]\n")
	for path, body := range map[string][]byte{"README.md": []byte("Public edition\n"), "private.txt": []byte("Synthetic private fixture\n"), ".vitryna.yml": configBytes} {
		if err := os.WriteFile(filepath.Join(source, path), body, 0644); err != nil {
			panic(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Synthetic source"}} {
		if err := exec.Command("git", append([]string{"-C", source}, args...)...).Run(); err != nil {
			panic(err)
		}
	}
	ctx := context.Background()
	snapshot, err := export.Open(ctx, source, "HEAD")
	if err != nil {
		panic(err)
	}
	committedConfig, err := snapshot.ReadFile(ctx, ".vitryna.yml", config.MaxBytes)
	if err != nil {
		panic(err)
	}
	cfg, err := config.Decode(committedConfig)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(committedConfig)
	receipt, err := snapshot.Export(ctx, export.ExportRequest{Policy: cfg.ExportPolicy(), ToolVersion: "example-v1", ConfigSHA256: hex.EncodeToString(digest[:]), OutputPath: filepath.Join(root, "public"), ReceiptPath: filepath.Join(root, "receipt.json")})
	if err != nil {
		panic(err)
	}
	if err := export.VerifyTree(ctx, filepath.Join(root, "public"), receipt); err != nil {
		panic(err)
	}
	fmt.Println(receipt.Files[0].Path, receipt.Files[0].Mode, receipt.FileCount == 1)
	// Output: README.md 100644 true
}
