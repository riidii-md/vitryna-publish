package githubpublish

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/export"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func fileFixture(t *testing.T, dir, path, body string, mode os.FileMode) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(full, mode); err != nil {
		t.Fatal(err)
	}
}

func targetFixture(t *testing.T, format ...string) (string, string, string, export.Receipt) {
	t.Helper()
	public := t.TempDir()
	initArgs := []string{"init", "-q", "-b", "main"}
	bareArgs := []string{"init", "--bare", "-q"}
	if len(format) > 0 {
		initArgs = append(initArgs, "--object-format="+format[0])
		bareArgs = append(bareArgs, "--object-format="+format[0])
	}
	runGit(t, public, initArgs...)
	fileFixture(t, public, "old.md", "public baseline\n", 0644)
	runGit(t, public, "add", ".")
	runGit(t, public, "commit", "-qm", "public base")
	remote := t.TempDir()
	runGit(t, remote, bareArgs...)
	runGit(t, public, "push", "-q", remote, "main")
	base := runGit(t, public, "rev-parse", "HEAD")
	source := t.TempDir()
	runGit(t, source, "init", "-q", "-b", "main")
	fileFixture(t, source, "README.txt", "raw\r\nbytes\r\n", 0644)
	fileFixture(t, source, "bin/run", "#!/bin/sh\n", 0755)
	fileFixture(t, source, "private", "private fixture\n", 0644)
	fileFixture(t, source, ".vitryna.yml", "private control\n", 0644)
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-qm", "private fixture")
	// Introduce attributes only after the raw CRLF blob is committed; publication must not apply them again.
	fileFixture(t, source, ".gitattributes", "*.txt text eol=lf\n", 0644)
	runGit(t, source, "add", ".gitattributes")
	runGit(t, source, "commit", "-qm", "attribute fixture")
	snapshot, err := export.Open(context.Background(), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	tree := filepath.Join(dest, "tree")
	receipt, err := snapshot.Export(context.Background(), export.ExportRequest{Policy: export.Policy{PublicPaths: []string{"README.txt", "bin", ".gitattributes"}, PrivatePaths: []string{"private"}}, ToolVersion: "test", ConfigSHA256: hex.EncodeToString(make([]byte, 32)), OutputPath: tree, ReceiptPath: filepath.Join(dest, "receipt.json")})
	if err != nil {
		t.Fatal(err)
	}
	return remote, base, tree, receipt
}

func TestPublicBaseMovementAndNoDiff(t *testing.T) {
	remote, base, tree, receipt := targetFixture(t)
	opts := TargetGitOptions{Remote: remote, BaseBranch: "main", PromotionBranch: "vitryna/public", BaseCommit: base, BotID: 7, BotLogin: "fixture[bot]"}
	first, err := NewTargetGit(context.Background(), opts, "")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	prepared, err := first.Prepare(context.Background(), tree, receipt, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Push(context.Background(), prepared.Commit, "", "main"); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "update-ref", "refs/heads/main", prepared.Commit, base)
	stale, err := NewTargetGit(context.Background(), opts, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	if _, err := stale.Prepare(context.Background(), tree, receipt, prepared.Commit); err != ErrBaseMoved {
		t.Fatalf("base movement not detected: %v", err)
	}
	opts.BaseCommit = prepared.Commit
	current, err := NewTargetGit(context.Background(), opts, "")
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	equal, err := current.Prepare(context.Background(), tree, receipt, prepared.Commit)
	if err != nil || equal.HasDiff || equal.Commit != "" {
		t.Fatalf("no-diff not recognized: %#v %v", equal, err)
	}
	if _, err := current.Prepare(context.Background(), tree, receipt, base); err == nil {
		t.Fatal("changed promotion head accepted")
	}
}

func TestSecretlessCompareCannotCreateCommitOrPush(t *testing.T) {
	remote, base, tree, receipt := targetFixture(t)
	git, err := NewTargetGit(context.Background(), TargetGitOptions{Remote: remote, BaseBranch: "main", PromotionBranch: "vitryna/public", BaseCommit: base, ReadOnly: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer git.Close()
	prepared, err := git.Prepare(context.Background(), tree, receipt, "")
	if err != nil || !prepared.HasDiff || prepared.Commit != "" {
		t.Fatalf("secretless comparison %#v %v", prepared, err)
	}
	if err := git.Push(context.Background(), base, "", "main"); err == nil {
		t.Fatal("read-only comparison pushed")
	}
	if _, err := NewTargetGit(context.Background(), TargetGitOptions{Remote: remote, BaseBranch: "main", PromotionBranch: "vitryna/public", BaseCommit: base, ReadOnly: true}, "synthetic-token"); err == nil {
		t.Fatal("secretless compare accepted target token")
	}
}

func TestSHA256PublicObjectFormat(t *testing.T) {
	remote, base, tree, receipt := targetFixture(t, "sha256")
	if len(base) != 64 || len(receipt.SourceCommit) != 40 {
		t.Fatal("cross-object-format fixture invalid")
	}
	git, err := NewTargetGit(context.Background(), TargetGitOptions{Remote: remote, BaseBranch: "main", PromotionBranch: "vitryna/public", BaseCommit: base, BotID: 7, BotLogin: "fixture[bot]"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer git.Close()
	prepared, err := git.Prepare(context.Background(), tree, receipt, "")
	if err != nil || len(prepared.Commit) != 64 {
		t.Fatalf("SHA256 build: %v", err)
	}
	if err := git.Push(context.Background(), prepared.Commit, "", "main"); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, remote, "rev-list", "--parents", "-n", "1", "refs/heads/vitryna/public"); got != prepared.Commit+" "+base {
		t.Fatal("SHA256 ancestry mismatch")
	}
}

func TestTargetOnlyCommitPreservesBytesAndAncestry(t *testing.T) {
	remote, base, tree, receipt := targetFixture(t)
	git, err := NewTargetGit(context.Background(), TargetGitOptions{Remote: remote, BaseBranch: "main", PromotionBranch: "vitryna/public", BaseCommit: base, BotID: 7, BotLogin: "fixture[bot]"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer git.Close()
	prepared, err := git.Prepare(context.Background(), tree, receipt, "")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.HasDiff {
		t.Fatal("changed tree treated as equal")
	}
	if err := git.Push(context.Background(), prepared.Commit, "", "main"); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, remote, "rev-list", "--parents", "-n", "1", "refs/heads/vitryna/public"); got != prepared.Commit+" "+base {
		t.Fatalf("wrong ancestry: %s", got)
	}
	if got := runGit(t, remote, "rev-parse", "refs/heads/main"); got != base {
		t.Fatal("default branch changed")
	}
	cmd := exec.Command("git", "-C", remote, "show", prepared.Commit+":README.txt")
	data, err := cmd.Output()
	if err != nil || string(data) != "raw\r\nbytes\r\n" {
		t.Fatalf("attributes transformed public bytes: %q %v", data, err)
	}
	if got := runGit(t, remote, "ls-tree", "-r", prepared.Commit); !strings.Contains(got, "100755 blob") || strings.Contains(got, "private") || strings.Contains(got, ".vitryna.yml") {
		t.Fatalf("unsafe public tree: %s", got)
	}
	if got := runGit(t, remote, "show", "-s", "--format=%an%n%ae%n%B", prepared.Commit); !strings.Contains(got, "fixture[bot]\n7+fixture[bot]@users.noreply.github.com") || strings.Contains(got, receipt.SourceCommit) || !strings.HasSuffix(got, "Vitryna-Publish: v1") {
		t.Fatalf("unsafe commit identity/message: %s", got)
	}
	if err := exec.Command("git", "-C", remote, "cat-file", "-e", receipt.SourceCommit).Run(); err == nil {
		t.Fatal("private source commit entered public object store")
	}
	competing, err := git.oid(context.Background(), strings.NewReader(CommitMessage), "commit-tree", prepared.Tree, "-p", prepared.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Push(context.Background(), competing, base, "main"); err == nil {
		t.Fatal("mismatching explicit lease accepted")
	}
	if got := runGit(t, remote, "rev-parse", "refs/heads/vitryna/public"); got != prepared.Commit {
		t.Fatal("lease failure changed remote head")
	}
	if err := git.Push(context.Background(), "", prepared.Commit, "vitryna/public"); err == nil {
		t.Fatal("current default branch deleted")
	}
	if err := git.Push(context.Background(), "", prepared.Commit, "main"); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", remote, "rev-parse", "--verify", "refs/heads/vitryna/public").Run(); err == nil {
		t.Fatal("owned promotion branch not deleted")
	}
}
