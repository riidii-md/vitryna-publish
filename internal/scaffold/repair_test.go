package scaffold

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"go.yaml.in/yaml/v3"
)

func TestInitLargeInventoryChargesOnlyPrivateRules(t *testing.T) {
	for _, visibility := range []string{"public", "private"} {
		t.Run(visibility, func(t *testing.T) {
			root := setupFixture(t)
			git := func(input string, args ...string) string {
				t.Helper()
				command := exec.Command("git", append([]string{"-C", root}, args...)...)
				command.Stdin = strings.NewReader(input)
				data, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, data)
				}
				return strings.TrimSpace(string(data))
			}
			blob := git("x", "hash-object", "-w", "--stdin")
			var index strings.Builder
			metadataBytes := 0
			for i := 0; i < 2000; i++ {
				path := fmt.Sprintf("%s/%s/%s/%s/%05d.txt", visibility, strings.Repeat("a", 180), strings.Repeat("b", 180), strings.Repeat("c", 180), i)
				metadataBytes += len(path) + 5
				fmt.Fprintf(&index, "100644 %s\t%s\x00", blob, path)
			}
			if metadataBytes <= config.MaxBytes {
				t.Fatal("fixture does not cross inventory/config distinction")
			}
			git(index.String(), "update-index", "-z", "--index-info")
			git("", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Synthetic large inventory")
			options := setupOptions(root)
			if visibility == "public" {
				options.PublicPaths = append(options.PublicPaths, "public")
			}
			_, err := Init(context.Background(), options)
			if visibility == "private" {
				if err == nil {
					t.Fatal("oversized private configuration accepted")
				}
				for _, name := range []string{".vitryna.yml", ".vitryna-setup.md", ".github/workflows/vitryna-publish.yml"} {
					if _, statErr := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(statErr) {
						t.Fatalf("failed setup exposed %s", name)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("compact public-subtree setup refused: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, ".vitryna.yml"))
			if err != nil || len(data) > config.MaxBytes {
				t.Fatalf("generated config: %d %v", len(data), err)
			}
			cfg, err := config.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Export.PublicPaths, []string{"README.md", "cmd/demo", "public"}) || len(cfg.Export.PrivatePaths) != 4 {
				t.Fatalf("narrow rules changed: %#v", cfg.Export)
			}
			snapshot, err := export.Open(context.Background(), root, "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			check, err := snapshot.Check(context.Background(), cfg.ExportPolicy())
			if err != nil || len(check.Paths) != 2003 {
				t.Fatalf("incomplete classification: %d %v", len(check.Paths), err)
			}
		})
	}
}

func TestWorkflowCommandLineSeparators(t *testing.T) {
	for _, row := range []struct {
		name, separator string
		accepted        bool
	}{
		{"LF", "\n", true}, {"CRLF", "\r\n", true}, {"CR", "\r", false},
		{"NEL", "\u0085", false}, {"LS", "\u2028", false}, {"PS", "\u2029", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			options := setupOptions("unused")
			options.VerifyCommand = "echo first" + row.separator + "echo second"
			data, err := RenderWorkflow(options)
			if !row.accepted {
				if err == nil || len(data) != 0 {
					t.Fatal("unsupported separator accepted by rendering")
				}
				options.Repository = setupFixture(t)
				if _, err := Init(context.Background(), options); err == nil {
					t.Fatal("unsupported separator accepted by init")
				}
				for _, name := range []string{".vitryna.yml", ".vitryna-setup.md", ".github/workflows/vitryna-publish.yml"} {
					if _, err := os.Lstat(filepath.Join(options.Repository, name)); !os.IsNotExist(err) {
						t.Fatalf("refusal exposed %s", name)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got parsedWorkflow
			if err := yaml.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			baseline := setupOptions("unused")
			baseline.VerifyCommand = "echo first\necho second"
			baselineData, err := RenderWorkflow(baseline)
			if err != nil {
				t.Fatal(err)
			}
			var want parsedWorkflow
			if err := yaml.Unmarshal(baselineData, &want); err != nil {
				t.Fatal(err)
			}
			if len(got.Jobs) != 4 || !reflect.DeepEqual(got, want) {
				t.Fatal("command changed surrounding workflow structure")
			}
			project := ""
			for _, step := range got.Jobs["verify"].Steps {
				if step.ID == "project" {
					project = step.Run
				}
			}
			if project != "echo first\necho second\n" {
				t.Fatalf("project command changed: %q", project)
			}
		})
	}
}
