package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowSealAndVerifyTree(t *testing.T) {
	ctx := context.Background()
	dir := inspectFixture(t)
	dest := t.TempDir()
	tree, receipt, bundle := filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json"), filepath.Join(dest, "bundle")
	for _, args := range [][]string{
		{"export", "--repository", dir, "--output", tree, "--receipt", receipt},
		{"workflow", "seal", "--tree", tree, "--receipt", receipt, "--output", bundle},
		{"workflow", "verify-artifact", "--tree", tree, "--receipt", receipt},
	} {
		var out, errs bytes.Buffer
		if code := Run(ctx, args, &out, &errs, "test"); code != 0 {
			t.Fatalf("%v exit%d %s", args, code, errs.String())
		}
	}
	for _, name := range []string{"tree.tar", "receipt.json"} {
		if _, err := os.Stat(filepath.Join(bundle, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("PUBLIC"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := Run(ctx, []string{"workflow", "verify-artifact", "--tree", tree, "--receipt", receipt}, &out, &errs, "test"); code != 1 || out.Len() != 0 {
		t.Fatalf("mutation accepted: %d %s", code, errs.String())
	}
}

func TestWorkflowArtifactArguments(t *testing.T) {
	for _, args := range [][]string{{"workflow", "seal"}, {"workflow", "verify-artifact", "--tree", "x"}, {"workflow", "verify-artifact", "--tree", "x", "--receipt", "r", "--archive", "z", "--sha256", "s", "--output", "o"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs, "test"); code != 2 {
			t.Fatalf("misuse exit %d: %s", code, errs.String())
		}
	}
}
