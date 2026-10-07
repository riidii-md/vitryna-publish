package ownedpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryCleanupPreservesReplacement(t *testing.T) {
	for _, scenario := range []string{"normal", "directory replacement", "symlink replacement"} {
		t.Run(scenario, func(t *testing.T) {
			parent := t.TempDir()
			owned, err := TempDir(parent, "owned-")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(owned.Path, "original"), []byte("owned bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "normal" {
				if err := owned.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(owned.Path); !os.IsNotExist(err) {
					t.Fatal("owned directory not removed")
				}
				if err := owned.Close(); err != nil {
					t.Fatal("cleanup not idempotent")
				}
				return
			}
			moved := owned.Path + "-moved"
			if err := os.Rename(owned.Path, moved); err != nil {
				t.Fatal(err)
			}
			replacement := owned.Path
			if scenario == "symlink replacement" {
				replacement = t.TempDir()
				if err := os.Symlink(replacement, owned.Path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(replacement, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(replacement, "sentinel"), []byte("foreign bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := owned.Close(); !errors.Is(err, ErrOwnershipChanged) {
				t.Fatalf("replacement not refused: %v", err)
			}
			if _, err := os.Lstat(owned.Path); err != nil {
				t.Fatalf("replacement path removed: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(replacement, "sentinel")); err != nil || string(data) != "foreign bytes" {
				t.Fatal("foreign sentinel changed")
			}
			if data, err := os.ReadFile(filepath.Join(moved, "original")); err != nil || string(data) != "owned bytes" {
				t.Fatal("moved original changed")
			}
		})
	}
}
