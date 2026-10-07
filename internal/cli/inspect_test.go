package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExplainCancellationDuringFinalEnumeration(t *testing.T) {
	dir := inspectFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	count, ready := filepath.Join(bin, "first"), filepath.Join(bin, "ready")
	script := "#!/bin/sh\ncase \" $* \" in *\" ls-tree \"*)\nif test -e " + quote(count) + "; then touch " + quote(ready) + "; exec sleep 5; else touch " + quote(count) + "; fi;; esac\nexec " + quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errs bytes.Buffer
	done := make(chan int, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	go func() {
		defer close(finished)
		done <- Run(ctx, []string{"explain", "--repository", dir, "--format", "json", "README.md"}, &out, &errs, "dev")
	}()
	setupWait := 30 * time.Second
	if end, ok := t.Deadline(); ok {
		if remaining := time.Until(end) - 7*time.Second; remaining < setupWait {
			setupWait = remaining
		}
	}
	if setupWait <= 0 {
		t.Fatal("test deadline leaves no setup time")
	}
	deadline := time.NewTimer(setupWait)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-tick.C:
		case code := <-done:
			t.Fatalf("explain ended before cancellation fixture: %d %s", code, errs.String())
		case <-deadline.C:
			t.Fatal("final enumeration did not start")
		}
	}
	cancel()
	select {
	case code := <-done:
		if code != 1 || out.Len() != 0 || !strings.Contains(errs.String(), "cancelled") {
			t.Fatalf("exit %d output %s stderr %s", code, out.String(), errs.String())
		}
	case <-time.After(time.Second):
		// Let the bounded fixture finish before failing, keeping test-owned resources alive.
		<-done
		t.Fatal("final explanation enumeration ignored cancellation")
	}
}

func inspectFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"README.md": "public", "private": "secret", ".vitryna.yml": "version: 1\nexport:\n  public: [README.md]\n  private: [private]\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-qm", "fixture"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture Git: %v %s", err, data)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".vitryna.yml"), []byte("dirty: invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInspectionCLICommittedConfig(t *testing.T) {
	dir := inspectFixture(t)
	for _, args := range [][]string{
		{"check", "--repository", dir, "--format", "json"},
		{"explain", "--repository", dir, "--format", "json", "README.md"},
	} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs, "dev"); code != 0 {
			t.Fatalf("%v: exit=%d %s", args, code, errs.String())
		}
		var value map[string]any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if args[0] == "check" && len(value["paths"].([]any)) != 3 {
			t.Fatalf("bad check: %s", out.String())
		}
		if args[0] == "explain" && value["visibility"] != "public" {
			t.Fatalf("bad explanation: %s", out.String())
		}
		if strings.Contains(out.String(), "secret") {
			t.Fatal("blob body leaked")
		}
	}
}

func TestInspectionArgumentMisuse(t *testing.T) {
	for _, args := range [][]string{{"check", "extra"}, {"explain"}, {"check", "--format", "xml"}, {"explain", "one", "two"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs, "dev"); code != 2 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, errs.String())
		}
	}
}
