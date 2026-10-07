package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/riidii-md/vitryna-publish/export"
)

func TestExportCLI(t *testing.T) {
	dir := inspectFixture(t)
	dest := t.TempDir()
	outPath, receiptPath := filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json")
	var out, errs bytes.Buffer
	code := Run(context.Background(), []string{"export", "--repository", dir, "--output", outPath, "--receipt", receiptPath, "--format", "json"}, &out, &errs, "cli-test")
	if code != 0 {
		t.Fatalf("export exit=%d: %s", code, errs.String())
	}
	var receipt export.Receipt
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ToolVersion != "cli-test" || receipt.FileCount != 1 {
		t.Fatalf("bad receipt: %#v", receipt)
	}
	if err := export.VerifyTree(context.Background(), outPath, receipt); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(outPath, "README.md"))
	if err != nil || string(body) != "public" {
		t.Fatalf("exported %q: %v", body, err)
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(data, out.Bytes()) {
		t.Fatal("stdout differs from sealed receipt")
	}
}

func TestExportArgumentMisuse(t *testing.T) {
	for _, args := range [][]string{{"export"}, {"export", "--output", "out"}, {"export", "--output", "out", "--receipt", "receipt", "extra"}, {"export", "--output", "out", "--receipt", "receipt", "--format", "xml"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs, "dev"); code != 2 {
			t.Fatalf("%v: exit=%d %s", args, code, errs.String())
		}
	}
}
