package export

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartialCloneDoesNotFetchOrInvokeHelper(t *testing.T) {
	origin := fixture(t)
	git(t, origin, "config", "uploadpack.allowFilter", "true")
	parent := t.TempDir()
	clone := filepath.Join(parent, "clone")
	git(t, parent, "clone", "--no-checkout", "--filter=blob:none", "file://"+origin, clone)
	before := objectFiles(t, clone)
	helperDir := t.TempDir()
	marker := filepath.Join(helperDir, "invoked")
	helper := "#!/bin/sh\nprintf invoked > '" + marker + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(helperDir, "git-remote-vitryntrap"), []byte(helper), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", helperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	git(t, clone, "config", "remote.origin.url", "vitryntrap::unused")
	s, err := Open(context.Background(), clone, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadFile(context.Background(), "README.md", 1024); err == nil {
		t.Fatal("missing promisor blob accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("transport helper invoked: %v", err)
	}
	if after := objectFiles(t, clone); after != before {
		t.Fatal("source object database mutated")
	}
}

func objectFiles(t *testing.T, dir string) string {
	t.Helper()
	var manifest strings.Builder
	err := filepath.WalkDir(filepath.Join(dir, ".git", "objects"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&manifest, "%s:%x\n", path, sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest.String()
}

func TestSHA256CommitAndMissingRef(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "--object-format=sha256")
	writeFixture(t, dir, "file", "data")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "sha256 fixture")
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil || len(s.Commit()) != 64 {
		t.Fatalf("sha256 source: %#v %v", s, err)
	}
	if _, err := Open(context.Background(), dir, "--bad-ref"); err == nil {
		t.Fatal("option-like ref accepted")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	data, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	for path, data := range map[string]string{"README.md": "original\n", "private/secret": "private\n", ".vitryna.yml": "version: 1\nexport:\n  public: [README.md]\n  private: [private]\n"} {
		writeFixture(t, dir, path, data)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "fixture")
	return dir
}

func writeFixture(t *testing.T, dir, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, path), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedSnapshotIgnoresWorktreeAndReplacementObjects(t *testing.T) {
	dir := fixture(t)
	ctx := context.Background()
	s, err := Open(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := s.ReadFile(ctx, ".vitryna.yml", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "README.md", "replacement\n")
	writeFixture(t, dir, ".vitryna.yml", "private: false\n")
	writeFixture(t, dir, "untracked", "not committed")
	git(t, dir, "add", "README.md", ".vitryna.yml")
	git(t, dir, "commit", "-qm", "replacement")
	newCommit := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "replace", s.Commit(), newCommit)
	for _, p := range []string{"README.md", ".vitryna.yml"} {
		oldBlob := git(t, dir, "--no-replace-objects", "rev-parse", s.Commit()+":"+p)
		newBlob := git(t, dir, "--no-replace-objects", "rev-parse", newCommit+":"+p)
		git(t, dir, "replace", oldBlob, newBlob)
	}
	data, err := s.ReadFile(ctx, "README.md", 1024)
	if err != nil || string(data) != "original\n" {
		t.Fatalf("snapshot substituted: %q %v", data, err)
	}
	data, err = s.ReadFile(ctx, ".vitryna.yml", 1<<20)
	if err != nil || string(data) != string(configBefore) {
		t.Fatalf("policy substituted: %q %v", data, err)
	}
	p := Policy{PublicPaths: []string{"README.md"}, PrivatePaths: []string{"private"}}
	r, err := s.Check(ctx, p)
	if err != nil || len(r.Paths) != 3 || r.SourceCommit != s.Commit() {
		t.Fatalf("bad committed check: %#v %v", r, err)
	}
	e, err := s.Explain(context.Background(), p, "README.md")
	if err != nil || !e.Tracked || e.Visibility != "public" {
		t.Fatalf("explain: %#v %v", e, err)
	}
	e, err = s.Explain(context.Background(), p, "README.md/future")
	if err != nil || e.Tracked || e.Visibility != "public" {
		t.Fatalf("subtree explain: %#v %v", e, err)
	}
	if _, err = s.ReadFile(ctx, "untracked", 1024); err == nil {
		t.Fatal("read untracked file")
	}
	if _, err = s.ReadFile(ctx, "README.md", 2); err == nil {
		t.Fatal("read exceeded cap")
	}
}

func TestClassificationFailuresAndPrivateSpecialModes(t *testing.T) {
	dir := fixture(t)
	if err := os.Symlink("secret", filepath.Join(dir, "private", "link")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "private link")
	s, err := Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{PublicPaths: []string{"README.md"}, PrivatePaths: []string{"private"}}
	if _, err := s.Check(context.Background(), p); err != nil {
		t.Fatal("private link rejected:", err)
	}
	if _, err := s.Check(context.Background(), Policy{PublicPaths: []string{"missing"}, PrivatePaths: []string{"private", "README.md"}}); err == nil {
		t.Fatal("unmatched public rule accepted")
	}
	if _, err := s.Check(context.Background(), Policy{PublicPaths: []string{"README.md"}, PrivatePaths: []string{"absent"}}); err == nil {
		t.Fatal("unclassified path accepted")
	}
	blob := git(t, dir, "rev-parse", "HEAD:README.md")
	git(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+blob+",public/a")
	git(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+blob+",public/A")
	git(t, dir, "commit", "-qm", "case collision")
	s, err = Open(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), Policy{PublicPaths: []string{"public"}, PrivatePaths: []string{"private", "README.md"}}); err == nil {
		t.Fatal("collision accepted")
	}
}
