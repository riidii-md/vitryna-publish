package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLargeExactPolicyClassification(t *testing.T) {
	dir := fixture(t)
	writeFixture(t, dir, "seed", "x")
	blob := git(t, dir, "hash-object", "-w", "seed")
	policy := Policy{PublicPaths: []string{"README.md", "assets"}, PrivatePaths: []string{"private", "future", ".vitryna.yml/child"}}
	var index strings.Builder
	for i := 0; i < 6000; i++ {
		name := fmt.Sprintf("p%05d", i)
		policy.PrivatePaths = append(policy.PrivatePaths, name)
		fmt.Fprintf(&index, "100644 %s\t%s\x00", blob, name)
	}
	fmt.Fprintf(&index, "100755 %s\tassets/tools/demo\x00", blob)
	cmd := exec.Command("git", "-C", dir, "update-index", "-z", "--index-info")
	cmd.Stdin = strings.NewReader(index.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("index: %v %s", err, output)
	}
	git(t, dir, "commit", "-qm", "Large synthetic private inventory")
	ctx := context.Background()
	snapshot, err := Open(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	checked, err := snapshot.Check(ctx, policy)
	if err != nil || checked.SourceCommit != snapshot.Commit() || len(checked.Paths) != 6004 {
		t.Fatalf("complete check: %d %v", len(checked.Paths), err)
	}
	previous := ""
	for _, record := range checked.Paths {
		visibility, rule, mode := "private", record.Path, "100644"
		switch record.Path {
		case "README.md":
			visibility = "public"
		case "assets/tools/demo":
			visibility, rule, mode = "public", "assets", "100755"
		case "private/secret":
			rule = "private"
		}
		if record.Path <= previous || record.Visibility != visibility || record.Rule != rule || record.Mode != mode {
			t.Fatalf("classification/order: %#v", record)
		}
		previous = record.Path
	}
	for _, row := range []struct{ path, visibility, rule string }{{"future/new/file", "private", "future"}, {"assets/not-yet", "public", "assets"}, {".vitryna.yml", "private", ".vitryna.yml"}} {
		result, err := snapshot.Explain(ctx, policy, row.path)
		if err != nil || result.Visibility != row.visibility || result.Rule != row.rule || result.Tracked != (row.path == ".vitryna.yml") {
			t.Fatalf("explain %s: %#v %v", row.path, result, err)
		}
	}
	out := t.TempDir()
	receipt, err := snapshot.Export(ctx, ExportRequest{Policy: policy, ToolVersion: "test", ConfigSHA256: strings.Repeat("0", 64), OutputPath: filepath.Join(out, "tree"), ReceiptPath: filepath.Join(out, "receipt.json")})
	if err != nil || receipt.FileCount != 2 || receipt.Files[0].Path != "README.md" || receipt.Files[1].Path != "assets/tools/demo" || receipt.Files[1].Mode != "100755" {
		t.Fatalf("export selection: %#v %v", receipt, err)
	}
	if data, err := os.ReadFile(filepath.Join(out, "tree", "assets", "tools", "demo")); err != nil || string(data) != "x" {
		t.Fatalf("export bytes: %q %v", data, err)
	}
	writeFixture(t, dir, "zzz-unclassified", "private synthetic")
	git(t, dir, "add", "zzz-unclassified")
	git(t, dir, "commit", "-qm", "Late unclassified path")
	snapshot, err = Open(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic *Diagnostic
	if _, err := snapshot.Check(ctx, policy); !errors.As(err, &diagnostic) || diagnostic.Code != "policy_unclassified" || diagnostic.Path != "zzz-unclassified" {
		t.Fatalf("unclassified path accepted: %v", err)
	}
}
