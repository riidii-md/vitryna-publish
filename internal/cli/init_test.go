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

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/internal/scaffold"
)

func TestInitOfflineThenCommittedInspection(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{"README.md": "Public fixture\n", "private.txt": "Private fixture\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		if data, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v %s", err, data)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Synthetic source")
	git("remote", "add", "origin", "git@github.com:example/source.git")
	pins := scaffold.ReleasePins{Version: "v0.1.0-fixture", ArtifactFormat: "github-zip-bundle-v1", Binaries: map[string]string{"linux-amd64": strings.Repeat("a", 64), "linux-arm64": strings.Repeat("b", 64), "darwin-amd64": strings.Repeat("c", 64), "darwin-arm64": strings.Repeat("d", 64)}}
	encoded, err := json.Marshal(pins)
	if err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pins.json")
	if err := os.WriteFile(pinPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"init", "github", "--repository", root, "--target", "example/public", "--public", "README.md", "--verify-command", "go test ./...", "--release-version", pins.Version, "--pins", pinPath, "--offline"}
	var out, errs bytes.Buffer
	if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 0 || !strings.Contains(out.String(), "unchecked") {
		t.Fatalf("init exit%d %s %s", exit, out.String(), errs.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".vitryna.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Decode(data); err != nil {
		t.Fatal(err)
	}
	git("add", ".vitryna.yml", ".vitryna-setup.md", ".github/workflows/vitryna-publish.yml")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Reviewed setup")
	out.Reset()
	errs.Reset()
	if exit := Run(context.Background(), []string{"check", "--repository", root}, &out, &errs, "test"); exit != 0 {
		t.Fatalf("generated classification exit%d %s", exit, errs.String())
	}
	out.Reset()
	errs.Reset()
	if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 0 || !strings.Contains(out.String(), "already matches") {
		t.Fatalf("compatible init exit%d %s %s", exit, out.String(), errs.String())
	}
}

func TestInitRejectsUnreleasedOrMismatchedGeneration(t *testing.T) {
	for _, scenario := range []struct {
		args    []string
		version string
		exit    int
	}{
		{[]string{"init"}, "test", 2},
		{[]string{"init", "other"}, "test", 2},
		{[]string{"init", "github", "--target", "example/public", "--public", "README.md", "--offline"}, "dev", 1},
		{[]string{"init", "github", "--target", "example/public", "--public", "README.md", "--release-version", "v0.2.0", "--offline"}, "v0.1.0", 1},
	} {
		var out, errs bytes.Buffer
		if exit := Run(context.Background(), scenario.args, &out, &errs, scenario.version); exit != scenario.exit {
			t.Fatalf("init misuse %v exit%d %s", scenario.args, exit, errs.String())
		}
	}
}
