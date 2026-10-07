package export

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaseAliasCannotExportIntoSource(t *testing.T) {
	dir := fixture(t)
	alias := strings.ToUpper(dir)
	actual, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Stat(alias)
	if err != nil || !os.SameFile(actual, other) {
		t.Skip("filesystem has no case alias")
	}
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	req := exportRequest(t, s, filepath.Join(alias, "unsafe-output"), filepath.Join(t.TempDir(), "receipt.json"))
	if _, err := s.Export(context.Background(), req); err == nil {
		t.Fatal("case-aliased source containment bypassed")
	}
	if _, err := os.Lstat(filepath.Join(dir, "unsafe-output")); !os.IsNotExist(err) {
		t.Fatal("wrote inside source")
	}
}

func TestMarkerAcrossCopyBoundaryAndLFS(t *testing.T) {
	for _, body := range []string{
		strings.Repeat("x", 32767) + "FORBIDDEN" + strings.Repeat("x", 100),
		"version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 3\n",
	} {
		dir := fixture(t)
		writeFixture(t, dir, "README.md", body)
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-qm", "rejected content")
		s, err := Open(context.Background(), dir, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		dest := t.TempDir()
		req := exportRequest(t, s, filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json"))
		req.Policy.ForbiddenContent = []string{"FORBIDDEN"}
		if _, err := s.Export(context.Background(), req); err == nil {
			t.Fatal("unsafe content accepted")
		}
		if _, err := os.Lstat(req.OutputPath); !os.IsNotExist(err) {
			t.Fatal("failed content exposed final tree")
		}
	}
}

func TestBlobLimitBeforePublication(t *testing.T) {
	dir := fixture(t)
	file, err := os.OpenFile(filepath.Join(dir, "README.md"), os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxBlobBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "oversized")
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	req := exportRequest(t, s, filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json"))
	if _, err := s.Export(context.Background(), req); err == nil {
		t.Fatal("oversized blob accepted")
	}
	if _, err := os.Lstat(req.OutputPath); !os.IsNotExist(err) {
		t.Fatal("oversized blob exposed output")
	}
}

func exportRequest(t *testing.T, s *Snapshot, out, receipt string) ExportRequest {
	t.Helper()
	data, err := s.ReadFile(context.Background(), ".vitryna.yml", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return ExportRequest{Policy: Policy{PublicPaths: []string{"README.md"}, PrivatePaths: []string{"private"}}, ToolVersion: "test", ConfigSHA256: hex.EncodeToString(digest[:]), OutputPath: out, ReceiptPath: receipt}
}

func TestDeterministicExportAndVerification(t *testing.T) {
	dir := fixture(t)
	if err := os.Chmod(filepath.Join(dir, "README.md"), 0755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "executable")
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var receipts [][]byte
	for i := 0; i < 2; i++ {
		dest := t.TempDir()
		req := exportRequest(t, s, filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json"))
		r, err := s.Export(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if r.SourceCommit != s.Commit() || r.FileCount != 1 || r.Files[0].Mode != "100755" || r.TotalBytes != 9 {
			t.Fatalf("bad receipt: %#v", r)
		}
		if err := VerifyTree(context.Background(), req.OutputPath, r); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(req.ReceiptPath)
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, data)
		if !bytes.HasSuffix(data, []byte("\n")) || !bytes.Contains(data, []byte("  \"schemaVersion\": 1")) {
			t.Fatal("receipt encoding changed")
		}
		if _, err := os.Stat(filepath.Join(req.OutputPath, "private")); !os.IsNotExist(err) {
			t.Fatal("private content exported")
		}
		if err := os.WriteFile(filepath.Join(req.OutputPath, "README.md"), []byte("tampered"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := VerifyTree(context.Background(), req.OutputPath, r); err == nil {
			t.Fatal("tampered tree accepted")
		}
	}
	if !bytes.Equal(receipts[0], receipts[1]) {
		t.Fatal("receipts are nondeterministic")
	}
}

func TestDestinationRefusalAndMarkerFailure(t *testing.T) {
	dir := fixture(t)
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"source output", "source receipt", "receipt inside tree", "existing receipt", "nonempty output", "symlink output", "marker"} {
		t.Run(scenario, func(t *testing.T) {
			dest := t.TempDir()
			req := exportRequest(t, s, filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json"))
			switch scenario {
			case "source output":
				req.OutputPath = filepath.Join(dir, "out")
			case "source receipt":
				req.ReceiptPath = filepath.Join(dir, "receipt.json")
			case "receipt inside tree":
				req.ReceiptPath = filepath.Join(req.OutputPath, "receipt.json")
			case "existing receipt":
				if err := os.WriteFile(req.ReceiptPath, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "nonempty output":
				writeFixture(t, req.OutputPath, "keep", "keep")
			case "symlink output":
				if err := os.Symlink(dir, req.OutputPath); err != nil {
					t.Fatal(err)
				}
			case "marker":
				req.Policy.ForbiddenContent = []string{"original"}
			}
			if _, err := s.Export(context.Background(), req); err == nil {
				t.Fatal("unsafe export succeeded")
			}
			if scenario != "existing receipt" {
				if _, err := os.Lstat(req.ReceiptPath); !os.IsNotExist(err) {
					t.Fatalf("failure issued receipt: %v", err)
				}
			}
			if scenario == "existing receipt" {
				b, _ := os.ReadFile(req.ReceiptPath)
				if string(b) != "keep" {
					t.Fatal("overwrote user receipt")
				}
			}
			if scenario == "nonempty output" {
				b, _ := os.ReadFile(filepath.Join(req.OutputPath, "keep"))
				if string(b) != "keep" {
					t.Fatal("removed user output")
				}
			}
		})
	}
}

func TestEmptyOutputAndReceiptFailureRollback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires unprivileged user")
	}
	dir := fixture(t)
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	out := filepath.Join(dest, "tree")
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	original, _ := os.Stat(out)
	receiptParent := filepath.Join(dest, "receipt")
	if err := os.Mkdir(receiptParent, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(receiptParent, 0700) })
	req := exportRequest(t, s, out, filepath.Join(receiptParent, "receipt.json"))
	if _, err := s.Export(context.Background(), req); err == nil {
		t.Fatal("receipt failure reported success")
	}
	current, err := os.Stat(out)
	if err != nil || !os.SameFile(original, current) {
		t.Fatalf("user empty output not restored: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed export left files: %v %v", entries, err)
	}
}
