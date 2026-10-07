package export

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verifiedFixture(t *testing.T) (string, Receipt) {
	t.Helper()
	s, err := Open(context.Background(), fixture(t), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	tree := filepath.Join(dest, "tree")
	r, err := s.Export(context.Background(), exportRequest(t, s, tree, filepath.Join(dest, "receipt.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyTree(context.Background(), tree, r); err != nil {
		t.Fatal(err)
	}
	return tree, r
}

func TestVerifyTreeSpecialModes(t *testing.T) {
	for name, flag := range map[string]os.FileMode{"sticky": os.ModeSticky, "setuid": os.ModeSetuid, "setgid": os.ModeSetgid} {
		t.Run(name, func(t *testing.T) {
			tree, r := verifiedFixture(t)
			path := filepath.Join(tree, "README.md")
			if err := os.Chmod(path, 0644|flag); err != nil {
				t.Skipf("unsupported permission flag: %v", err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&flag == 0 {
				t.Skip("filesystem stripped individual permission flag")
			}
			var d *Diagnostic
			if err := VerifyTree(context.Background(), tree, r); !errors.As(err, &d) || d.Code != "verify_file_metadata" {
				t.Fatalf("special mode accepted: %v", err)
			}
		})
	}
}

func TestVerifyTreeIntegrityMatrix(t *testing.T) {
	for scenario, code := range map[string]string{"same-length content": "verify_file_digest", "mode": "verify_file_metadata", "missing": "verify_missing_file", "extra file": "verify_extra_path", "extra directory": "verify_extra_path", "total": "receipt_tree_digest", "tree digest": "receipt_tree_digest", "file digest": "receipt_tree_digest"} {
		t.Run(scenario, func(t *testing.T) {
			tree, r := verifiedFixture(t)
			path := filepath.Join(tree, "README.md")
			var err error
			switch scenario {
			case "same-length content":
				err = os.WriteFile(path, []byte("tampered\n"), 0644)
			case "mode":
				err = os.Chmod(path, 0755)
			case "missing":
				err = os.Remove(path)
			case "extra file":
				err = os.WriteFile(filepath.Join(tree, "extra"), nil, 0644)
			case "extra directory":
				err = os.Mkdir(filepath.Join(tree, "extra"), 0700)
			case "total":
				r.TotalBytes++
			case "tree digest":
				r.TreeSHA256 = strings.Repeat("0", 64)
			case "file digest":
				r.Files[0].SHA256 = strings.Repeat("0", 64)
			}
			if err != nil {
				t.Fatal(err)
			}
			var d *Diagnostic
			if err := VerifyTree(context.Background(), tree, r); !errors.As(err, &d) || d.Code != code {
				t.Fatalf("want %s, got %v", code, err)
			}
		})
	}
}
