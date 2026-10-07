package scaffold

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/config"
)

func setupFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{"README.md": "Public\n", "cmd/demo/main.go": "package main\n", "private/internal.md": "Synthetic private fixture\n"} {
		name := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Synthetic source"}} {
		if data, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("Git: %v %s", err, data)
		}
	}
	return root
}
func setupOptions(root string) Options {
	return Options{Repository: root, SourceRepository: "example/source", GitHub: config.GitHub{Repository: "example/public", BaseBranch: "main", PromotionBranch: "vitryna/public", TrustedSourceBranch: "main", Merge: config.Merge{Mode: "manual"}}, PublicPaths: []string{"README.md", "cmd/demo"}, VerifyCommand: "go test ./...", Pins: fixturePins()}
}

func TestInitClassifiesAndRefusesOverwrite(t *testing.T) {
	root := setupFixture(t)
	options := setupOptions(root)
	result, err := Init(context.Background(), options)
	if err != nil || len(result.Files) != 3 || result.Existing {
		t.Fatalf("init %#v %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".vitryna.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Export.PublicPaths, []string{"README.md", "cmd/demo"}) || !reflect.DeepEqual(cfg.Export.PrivatePaths, []string{".github/workflows/vitryna-publish.yml", ".vitryna-setup.md", ".vitryna.yml", "private/internal.md"}) {
		t.Fatalf("classification %#v", cfg.Export)
	}
	if again, err := Init(context.Background(), options); err != nil || !again.Existing {
		t.Fatalf("compatible setup %#v %v", again, err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vitryna.yml"), []byte("user-owned changed file"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(context.Background(), options); err == nil {
		t.Fatal("conflict overwritten")
	}
	got, err := os.ReadFile(filepath.Join(root, ".vitryna.yml"))
	if err != nil || string(got) != "user-owned changed file" {
		t.Fatal("existing file changed")
	}
}

func TestSetupSelectionAndTemplateInputRefusals(t *testing.T) {
	for _, scenario := range []string{"missing public", "overlap", "control", "private guide", "expression branch", "expression command", "existing partial", "symlink parent"} {
		t.Run(scenario, func(t *testing.T) {
			root := setupFixture(t)
			options := setupOptions(root)
			switch scenario {
			case "missing public":
				options.PublicPaths = []string{"missing"}
			case "overlap":
				options.PublicPaths = []string{"cmd", "cmd/demo"}
			case "control":
				options.PublicPaths = []string{".github"}
			case "private guide":
				options.PublicPaths = []string{".vitryna-setup.md"}
			case "expression branch":
				options.GitHub.BaseBranch = "${{secrets.KEY}}"
			case "expression command":
				options.VerifyCommand = "echo '${{ secrets.KEY }}'"
			case "existing partial":
				if err := os.WriteFile(filepath.Join(root, ".vitryna-setup.md"), []byte("existing"), 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink parent":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, ".github")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Init(context.Background(), options); err == nil {
				t.Fatal("unsafe setup accepted")
			}
			if _, err := os.Lstat(filepath.Join(root, ".vitryna.yml")); !os.IsNotExist(err) {
				t.Fatal("failed setup exposed config")
			}
		})
	}
}

func TestSetupRollbackPreservesUserFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".github"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "existing"), []byte("preserve"), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{".vitryna.yml": []byte("config"), ".github/workflows/vitryna-publish.yml": []byte("workflow"), ".vitryna-setup.md": []byte("guide")}
	links := 0
	err := writeFiles(context.Background(), root, files, func(from, to string) error {
		links++
		if links == 3 {
			return errors.New("injected link failure")
		}
		return os.Link(from, to)
	})
	if err == nil || links != 3 {
		t.Fatal("failure not injected")
	}
	for path := range files {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("owned partial retained %s", path)
		}
	}
	got, err := os.ReadFile(filepath.Join(root, ".github", "existing"))
	if err != nil || strings.TrimSpace(string(got)) != "preserve" {
		t.Fatal("user files affected")
	}
}
