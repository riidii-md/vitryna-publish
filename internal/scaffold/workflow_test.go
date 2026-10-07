package scaffold

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowStep struct {
	ID, Name, Uses, Run string
	Env                 map[string]string
	With                map[string]any `yaml:"with"`
	WorkingDirectory    string         `yaml:"working-directory"`
}
type workflowJob struct {
	If          string
	Needs       any
	Permissions map[string]string
	Outputs     map[string]string
	Steps       []workflowStep
}
type parsedWorkflow struct {
	On          map[string]any
	Permissions map[string]string
	Concurrency struct {
		Group  string
		Cancel bool `yaml:"cancel-in-progress"`
	}
	Jobs map[string]workflowJob
}

func TestWorkflowCredentialZonesAndOriginalSeal(t *testing.T) {
	options := setupOptions("unused")
	data, err := RenderWorkflow(options)
	if err != nil {
		t.Fatal(err)
	}
	var w parsedWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Jobs) != 4 || len(w.Permissions) != 0 || w.Concurrency.Cancel || !strings.Contains(w.Concurrency.Group, "github.repository") {
		t.Fatal("workflow topology/permissions/concurrency unsafe")
	}
	if _, ok := w.On["pull_request_target"]; ok {
		t.Fatal("privileged PR event")
	}
	for name, job := range w.Jobs {
		bootstrap := false
		for _, step := range job.Steps {
			if step.ID == "vitryna" {
				bootstrap = true
				if !strings.Contains(step.Run, options.Pins.Binaries["linux-amd64"]) || !strings.Contains(step.Run, "sha256sum --check") {
					t.Fatal("missing exact binary checksum")
				}
			}
			if step.Uses != "" && !regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9-]+@[0-9a-f]{40}$`).MatchString(step.Uses) {
				t.Fatalf("mutable action pin %s", step.Uses)
			}
			if strings.HasPrefix(step.Uses, "actions/checkout@") {
				if step.With["persist-credentials"] != false {
					t.Fatal("checkout persisted credential")
				}
			}
			combined := step.Run
			for _, value := range step.Env {
				combined += value
			}
			if name == "seal" || name == "verify" {
				if strings.Contains(combined, "secrets.") || strings.Contains(combined, "VITRYNA_APP_PRIVATE_KEY") {
					t.Fatalf("App secret in %s", name)
				}
			}
			if (name == "invalidate" || name == "reconcile") && strings.Contains(step.Run, "go test") {
				t.Fatal("project command in credentialed job")
			}
		}
		if !bootstrap {
			t.Fatalf("%s builds or lacks a pinned binary", name)
		}
	}
	seal := w.Jobs["seal"]
	if !strings.Contains(seal.If, "always()") || !strings.Contains(seal.If, "needs.invalidate.result == 'skipped'") || !strings.Contains(seal.Outputs["artifact-id"], "steps.upload.outputs.artifact-id") {
		t.Fatal("preview guard/seal output missing")
	}
	for _, step := range seal.Steps {
		if step.ID == "upload" {
			if step.With["retention-days"] != 7 || step.With["overwrite"] != false || step.With["archive"] != true {
				t.Fatal("artifact lifetime/immutability/format")
			}
			paths := step.With["path"].(string)
			if !strings.Contains(paths, "tree.tar") || !strings.Contains(paths, "receipt.json") {
				t.Fatal("sealed archive entries missing")
			}
		}
	}
	reconcile := w.Jobs["reconcile"]
	if strings.Contains(reconcile.If, "has_changes") || !strings.Contains(reconcile.If, "needs.verify.result == 'success'") {
		t.Fatal("no-diff reconciliation gated away")
	}
	for _, step := range reconcile.Steps {
		if strings.Contains(step.Run, "workflow reconcile") {
			if step.Env["ARTIFACT_ID"] != "${{ needs.seal.outputs.artifact-id }}" || step.Env["ARTIFACT_DIGEST"] != "${{ needs.seal.outputs.artifact-digest }}" {
				t.Fatal("verification job can select publication artifact")
			}
		}
	}
	verify := w.Jobs["verify"]
	project, pre, post, compare := -1, -1, -1, -1
	for i, step := range verify.Steps {
		switch step.ID {
		case "preverify":
			pre = i
			if !strings.Contains(step.Run, "receipt-sha256=") || !strings.Contains(step.Run, "GITHUB_OUTPUT") || !strings.Contains(step.Run, "sha256sum") {
				t.Fatal("original receipt identity not captured before project execution")
			}
		case "project":
			project = i
			if step.WorkingDirectory != "${{ runner.temp }}/vitryna-verify/tree" {
				t.Fatal("project runs outside disposable copy")
			}
			if len(step.Env) != 0 {
				t.Fatal("project step inherits explicit credentials")
			}
		case "postverify":
			post = i
			if step.Env["RECEIPT_SHA256"] != "${{ steps.preverify.outputs.receipt-sha256 }}" || !strings.Contains(step.Run, `--receipt-sha256 "$RECEIPT_SHA256"`) {
				t.Fatal("postverify trusts a project-writable receipt identity")
			}
		case "compare":
			compare = i
			if step.Env["RECEIPT_SHA256"] != "${{ steps.preverify.outputs.receipt-sha256 }}" || !strings.Contains(step.Run, `--receipt-sha256 "$RECEIPT_SHA256"`) {
				t.Fatal("preview lost original receipt binding")
			}
		}
	}
	if pre < 0 || project <= pre || post <= project || compare <= post {
		t.Fatal("disposable receipt validation/preview sequence missing")
	}
}

func TestWorkflowMissingVerificationFailsAndVariantFixtures(t *testing.T) {
	for _, mode := range []string{"manual", "rulesets", "classic"} {
		t.Run(mode, func(t *testing.T) {
			options := setupOptions("unused")
			options.VerifyCommand = ""
			if mode != "manual" {
				options.GitHub.Merge.Mode = "auto"
				options.GitHub.Merge.Method = "squash"
				options.GitHub.Merge.ProtectionInspection = "effective_rulesets"
				if mode == "classic" {
					options.GitHub.Merge.ProtectionInspection = "classic"
				}
			}
			data, err := RenderWorkflow(options)
			if err != nil {
				t.Fatal(err)
			}
			golden := sha256.Sum256(data)
			if hex.EncodeToString(golden[:]) != "3b3892426a45964985e3d377f8561810ca7c0094fe1001e94c64bfa59ed97e8a" {
				t.Fatal("generated workflow changed; inspect the complete rendered fixture before updating its golden digest")
			}
			var w parsedWorkflow
			if err := yaml.Unmarshal(data, &w); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, step := range w.Jobs["verify"].Steps {
				if step.ID == "project" {
					found = strings.Contains(step.Run, "exit 1") && strings.Contains(step.Run, "verification command")
				}
			}
			if !found {
				t.Fatal("missing verification passes silently")
			}
			if root := os.Getenv("VITRYNA_WORKFLOW_FIXTURE_DIR"); root != "" {
				if err := os.WriteFile(filepath.Join(root, mode+".yml"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
