package artifact

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/riidii-md/vitryna-publish/export"
)

func fixture(t *testing.T) (string, string, export.Receipt) {
	t.Helper()
	source := t.TempDir()
	for name, body := range map[string]string{"README.md": "public\n", "private": "not public", ".vitryna.yml": "private control", "cmd/run": "#!/bin/sh\n", "docs/документ.md": "unicode\n"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(source, "cmd/run"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"}} {
		cmd := exec.Command("git", append([]string{"-C", source}, args...)...)
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, data)
		}
	}
	snapshot, err := export.Open(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	tree, receiptPath := filepath.Join(dest, "tree"), filepath.Join(dest, "receipt.json")
	r, err := snapshot.Export(context.Background(), export.ExportRequest{Policy: export.Policy{PublicPaths: []string{"README.md", "cmd", "docs"}, PrivatePaths: []string{"private"}}, ToolVersion: "test", ConfigSHA256: hex.EncodeToString(make([]byte, 32)), OutputPath: tree, ReceiptPath: receiptPath})
	if err != nil {
		t.Fatal(err)
	}
	return tree, receiptPath, r
}

func providerZIP(t *testing.T, bundle string, change func(string, []byte) []byte) (string, string) {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	for _, name := range []string{"tree.tar", "receipt.json"} {
		data, err := os.ReadFile(filepath.Join(bundle, name))
		if err != nil {
			t.Fatal(err)
		}
		if change != nil {
			data = change(name, data)
		}
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "provider.zip")
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buffer.Bytes())
	return path, hex.EncodeToString(sum[:])
}

func TestSealDeterministicAndDisposableCopy(t *testing.T) {
	tree, receiptPath, receipt := fixture(t)
	var archives [][]byte
	for i := 0; i < 2; i++ {
		bundle := filepath.Join(t.TempDir(), "bundle")
		if err := Seal(context.Background(), tree, receiptPath, bundle); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(bundle, "tree.tar"))
		if err != nil {
			t.Fatal(err)
		}
		archives = append(archives, data)
		zipPath, digest := providerZIP(t, bundle, nil)
		dest := filepath.Join(t.TempDir(), "copy")
		got, err := Extract(context.Background(), zipPath, digest, dest)
		if err != nil {
			t.Fatal(err)
		}
		if got.TreeSHA256 != receipt.TreeSHA256 {
			t.Fatal("receipt binding changed")
		}
		if err := export.VerifyTree(context.Background(), filepath.Join(dest, "tree"), receipt); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dest, "tree", "README.md"), []byte("changed\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := export.VerifyTree(context.Background(), filepath.Join(dest, "tree"), receipt); err == nil {
			t.Fatal("verification copy mutation accepted")
		}
		original, err := os.ReadFile(filepath.Join(bundle, "tree.tar"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(original, data) {
			t.Fatal("copy mutation changed sealed archive")
		}
	}
	if !bytes.Equal(archives[0], archives[1]) {
		t.Fatal("tar is not deterministic")
	}
}

func TestRejectTamperingBeforeExposure(t *testing.T) {
	tree, receiptPath, _ := fixture(t)
	bundle := filepath.Join(t.TempDir(), "bundle")
	if err := Seal(context.Background(), tree, receiptPath, bundle); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"raw digest", "receipt unknown field", "receipt duplicate key", "tar traversal", "tar link", "tar duplicate", "tar mode", "tar content", "tar extra", "tar missing"} {
		t.Run(scenario, func(t *testing.T) {
			zipPath, digest := providerZIP(t, bundle, func(name string, data []byte) []byte {
				if name == "receipt.json" {
					if scenario == "receipt unknown field" {
						return bytes.Replace(data, []byte("{\n"), []byte("{\n  \"extra\": true,\n"), 1)
					}
					if scenario == "receipt duplicate key" {
						return bytes.Replace(data, []byte("{\n"), []byte("{\n  \"schemaVersion\": 1,\n"), 1)
					}
					return data
				}
				if len(scenario) < 4 || scenario[:4] != "tar " {
					return data
				}
				var out bytes.Buffer
				w := tar.NewWriter(&out)
				for _, file := range []struct{ name, body string }{{"README.md", "public\n"}, {"cmd/run", "#!/bin/sh\n"}, {"docs/документ.md", "unicode\n"}} {
					if scenario == "tar missing" && file.name == "README.md" {
						continue
					}
					h := &tar.Header{Name: file.name, Mode: 0644, Size: int64(len(file.body)), Typeflag: tar.TypeReg}
					if file.name == "cmd/run" {
						h.Mode = 0755
					}
					if file.name == "README.md" {
						switch scenario {
						case "tar traversal":
							h.Name = "../README.md"
						case "tar link":
							h.Typeflag = tar.TypeSymlink
							h.Linkname = "outside"
							h.Size = 0
						case "tar mode":
							h.Mode = 0600
						case "tar content":
							file.body = "tamper\n"
						}
					}
					if err := w.WriteHeader(h); err != nil {
						t.Fatal(err)
					}
					if h.Typeflag == tar.TypeReg {
						if _, err := w.Write([]byte(file.body)); err != nil {
							t.Fatal(err)
						}
					}
					if scenario == "tar duplicate" && file.name == "README.md" {
						if err := w.WriteHeader(h); err != nil {
							t.Fatal(err)
						}
						if _, err := w.Write([]byte(file.body)); err != nil {
							t.Fatal(err)
						}
					}
				}
				if scenario == "tar extra" {
					if err := w.WriteHeader(&tar.Header{Name: "extra", Mode: 0644, Typeflag: tar.TypeReg}); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				return out.Bytes()
			})
			if scenario == "raw digest" {
				digest = hex.EncodeToString(make([]byte, 32))
			}
			dest := filepath.Join(t.TempDir(), "extracted")
			if _, err := Extract(context.Background(), zipPath, digest, dest); err == nil {
				t.Fatal("tampered artifact accepted")
			}
			if _, err := os.Lstat(dest); !os.IsNotExist(err) {
				t.Fatal("failed extraction exposed destination")
			}
		})
	}
}
