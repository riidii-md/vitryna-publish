package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactBlobAndAggregateLimitsWithOwnedRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("real 512 MiB aggregate boundary")
	}
	source := fixture(t)
	public := filepath.Join(source, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(public, "f00")
	file, err := os.OpenFile(first, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxBlobBytes); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 8; i++ {
		if err := os.Link(first, filepath.Join(public, fmt.Sprintf("f%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	git(t, source, "add", ".")
	git(t, source, "commit", "-qm", "Exact aggregate boundary")
	snapshot, err := Open(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	req := exportRequest(t, snapshot, filepath.Join(root, "tree"), filepath.Join(root, "receipt.json"))
	req.Policy.PublicPaths = []string{"public"}
	req.Policy.PrivatePaths = []string{"README.md", "private"}
	receipt, err := snapshot.Export(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.FileCount != 8 || receipt.TotalBytes != 512<<20 {
		t.Fatal("exact total boundary did not succeed")
	}
	for _, record := range receipt.Files {
		if record.Size != 64<<20 {
			t.Fatal("exact blob boundary changed")
		}
	}
	writeFixture(t, source, "public/f99", "x")
	git(t, source, "add", ".")
	git(t, source, "commit", "-qm", "One byte past aggregate limit")
	snapshot, err = Open(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	req.OutputPath, req.ReceiptPath = filepath.Join(root, "overflow-tree"), filepath.Join(root, "overflow-receipt.json")
	_, err = snapshot.Export(context.Background(), req)
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != "public_total_limit" {
		t.Fatalf("wrong aggregate refusal: %v", err)
	}
	for _, path := range []string{req.OutputPath, req.ReceiptPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("over-limit export exposed final output")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".vitryna-tree-") {
			t.Fatal("owned failure stage leaked")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "tree/public/f00")); err != nil {
		t.Fatal("rollback removed prior successful output")
	}
}

func TestReceiptEncodingCapExactBoundary(t *testing.T) {
	base := Receipt{ToolVersion: "x"}
	encoded, err := receiptBytes(base)
	if err != nil {
		t.Fatal(err)
	}
	base.ToolVersion = strings.Repeat("x", int(MaxReceiptBytes)-len(encoded)+1)
	encoded, err = receiptBytes(base)
	if err != nil || int64(len(encoded)) != MaxReceiptBytes {
		t.Fatal("exact receipt byte cap rejected")
	}
	base.ToolVersion += "x"
	if _, err := receiptBytes(base); err == nil {
		t.Fatal("receipt cap plus one accepted")
	}
}
