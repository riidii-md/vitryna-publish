package gitcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceGitDoesNotInheritPublicationSecrets(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s%s%s' \"$VITRYNA_APP_PRIVATE_KEY\" \"$VITRYNA_SOURCE_TOKEN\" \"$GITHUB_TOKEN\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, name := range []string{"VITRYNA_APP_PRIVATE_KEY", "VITRYNA_SOURCE_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(name, "synthetic-private-value")
	}
	data, err := Read(context.Background(), "", 1024, "--version")
	if err != nil || len(data) != 0 {
		t.Fatal("source Git inherited publication credentials")
	}
}

func TestCapabilityFailsClosed(t *testing.T) {
	for _, test := range []struct {
		version string
		want    bool
	}{{"git version 2.44.9", false}, {"git version 2.45.0", true}, {"git version 2.53.0", true}, {"nonsense", false}} {
		t.Run(test.version, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nprintf '%s' '"+test.version+"'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			if err := Capability(context.Background()); (err == nil) != test.want {
				t.Fatalf("capability = %v", err)
			}
		})
	}
}

func TestBoundedAndCancelledReads(t *testing.T) {
	if _, err := Read(context.Background(), "", 1, "--version"); !errors.Is(err, ErrSourceRead) {
		t.Fatalf("bounded reply: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, "", 1024, "--version"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

type cancelOnFirstWrite struct {
	cancel context.CancelFunc
}

func (w cancelOnFirstWrite) Write(data []byte) (int, error) {
	w.cancel()
	return len(data), nil
}

func TestCopyCancellationAfterStreamingStarts(t *testing.T) {
	root := t.TempDir()
	run := func(input string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = baseEnvironment()
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("synthetic Git fixture failed: %v %s", err, out)
		}
		return out
	}
	run("", "init", "-q")
	const size = 8 << 20
	oid := strings.TrimSpace(string(run(strings.Repeat("x", size), "hash-object", "-w", "--stdin")))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	written, err := Copy(ctx, root, cancelOnFirstWrite{cancel}, size, "cat-file", "blob", oid)
	if !errors.Is(err, context.Canceled) || written <= 0 || written >= size {
		t.Fatalf("in-stream cancellation not observed: written=%d err=%v", written, err)
	}
}
