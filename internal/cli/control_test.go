package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/artifact"
)

type controlTransport func(*http.Request) (*http.Response, error)

func (f controlTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReconcileRevalidatesSealBeforeAppCredentials(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	for _, name := range []string{"GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_REPOSITORY_ID"} {
		t.Setenv(name, "")
	}
	t.Setenv("VITRYNA_SOURCE_TOKEN", "synthetic-source")
	t.Setenv("VITRYNA_APP_CLIENT_ID", "Iv1_synthetic")
	t.Setenv("VITRYNA_APP_PRIVATE_KEY", "deliberately invalid key")
	dir, sha := controlFixture(t)
	root := t.TempDir()
	tree, receiptPath := filepath.Join(root, "tree"), filepath.Join(root, "receipt.json")
	var out, errs bytes.Buffer
	if exit := Run(context.Background(), []string{"export", "--repository", dir, "--output", tree, "--receipt", receiptPath}, &out, &errs, "test"); exit != 0 {
		t.Fatalf("export %s", errs.String())
	}
	original, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt export.Receipt
	if err := json.Unmarshal(original, &receipt); err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	for _, scenario := range []string{"valid", "wrong policy", "raw tamper"} {
		t.Run(scenario, func(t *testing.T) {
			changed := receipt
			if scenario == "wrong policy" {
				changed.PolicySHA256 = strings.Repeat("b", 64)
			}
			encoded, err := json.MarshalIndent(changed, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(receiptPath, append(encoded, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			bundle := filepath.Join(t.TempDir(), "seal")
			if err := artifact.Seal(context.Background(), tree, receiptPath, bundle); err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			for _, name := range []string{"receipt.json", "tree.tar"} {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(bundle, name))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(archive.Bytes())
			digest := hex.EncodeToString(hash[:])
			payload := append([]byte(nil), archive.Bytes()...)
			if scenario == "raw tamper" {
				payload[0] ^= 1
			}
			appRequests := 0
			http.DefaultTransport = controlTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, ""
				switch r.URL.Path {
				case "/repos/example/source/git/ref/heads/main", "/repos/example/public/git/ref/heads/main":
					body = `{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + sha + `"}}`
				case "/repos/example/source":
					body = `{"id":1,"full_name":"example/source"}`
				case "/repos/example/source/actions/artifacts/3":
					body = fmt.Sprintf(`{"id":3,"size_in_bytes":%d,"expired":false,"expires_at":"2099-01-01T00:00:00Z","digest":"sha256:%s","workflow_run":{"id":2,"repository_id":1,"head_repository_id":1,"head_sha":"%s"}}`, len(payload), digest, sha)
				case "/repos/example/source/actions/artifacts/3/zip":
					body = string(payload)
				case "/repos/example/public":
					body = `{"id":7,"full_name":"example/public","private":false,"default_branch":"main"}`
				case "/repos/example/public/contents/.github/workflows":
					status, body = 404, "{}"
				default:
					appRequests++
					t.Fatalf("unexpected API request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			args := controlArgs(dir, sha)
			args[1] = "reconcile"
			args = append(args, "--source-repository-id", "1", "--run-id", "2", "--artifact-id", "3", "--artifact-digest", digest)
			out.Reset()
			errs.Reset()
			code := "github_app_key_invalid"
			if scenario == "wrong policy" {
				code = "publication_receipt_identity"
			}
			if scenario == "raw tamper" {
				code = "github_artifact_transport_digest"
			}
			if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 1 || !strings.Contains(errs.String(), code) || appRequests != 0 {
				t.Fatalf("seal-before-App gate exit%d %s", exit, errs.String())
			}
			if scenario == "valid" {
				out.Reset()
				errs.Reset()
				preview := filepath.Join(t.TempDir(), "preview")
				args := []string{"workflow", "verify-artifact", "--source-repository", "example/source", "--source-repository-id", "1", "--artifact-head-repository-id", "1", "--source-commit", sha, "--run-id", "2", "--artifact-id", "3", "--artifact-digest", digest, "--output", preview}
				if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 0 {
					t.Fatalf("provider preview exit%d %s", exit, errs.String())
				}
				if _, err := os.Stat(filepath.Join(preview, "tree", "README.md")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func controlFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := inspectFixture(t)
	data := "version: 1\nexport:\n  public: [README.md]\n  private: [private]\npublish:\n  github:\n    repository: example/public\n    base_branch: main\n    promotion_branch: vitryna/public\n    trusted_source_branch: main\n"
	if err := os.WriteFile(filepath.Join(dir, ".vitryna.yml"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", ".vitryna.yml"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "publication config"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v %s", err, data)
		}
	}
	commit, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(commit))
}

func controlArgs(dir, sha string) []string {
	return []string{"workflow", "invalidate", "--repository", dir, "--source-repository", "example/source", "--source-commit", sha, "--target", "example/public", "--base", "main", "--promotion", "vitryna/public", "--trusted-source", "main"}
}

func TestControlIdentityAndCredentialsFailBeforeNetwork(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	dir, sha := controlFixture(t)
	for _, name := range []string{"VITRYNA_SOURCE_TOKEN", "VITRYNA_APP_CLIENT_ID", "VITRYNA_APP_PRIVATE_KEY", "GITHUB_REPOSITORY", "GITHUB_SHA"} {
		t.Setenv(name, "")
	}
	for _, scenario := range []string{"missing credentials", "target changed", "source commit changed", "source repository changed"} {
		t.Run(scenario, func(t *testing.T) {
			args := controlArgs(dir, sha)
			code := "publication_credentials_missing"
			switch scenario {
			case "target changed":
				args[9] = "different/public"
				code = "publication_identity_mismatch"
			case "source commit changed":
				args[7] = strings.Repeat("b", 40)
				code = "publication_identity_mismatch"
			case "source repository changed":
				t.Setenv("GITHUB_REPOSITORY", "different/source")
				code = "publication_source_identity"
			}
			var out, errs bytes.Buffer
			if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 1 || out.Len() != 0 || !strings.Contains(errs.String(), code) {
				t.Fatalf("%s exit%d output %s error %s", scenario, exit, out.String(), errs.String())
			}
		})
	}
}

func TestControlArgumentMisuse(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	var out, errs bytes.Buffer
	if exit := Run(context.Background(), []string{"workflow", "invalidate"}, &out, &errs, "test"); exit != 2 {
		t.Fatalf("missing identity flags exit%d %s", exit, errs.String())
	}
}

func TestPublicationReceiptBoundToSelectedSourcePolicyAndTool(t *testing.T) {
	data := []byte("synthetic committed config")
	policy := export.Policy{PublicPaths: []string{"README.md"}, PrivatePaths: []string{"private"}}
	hash := sha256.Sum256(data)
	receipt := export.Receipt{SourceCommit: strings.Repeat("a", 40), ConfigSHA256: hex.EncodeToString(hash[:]), PolicySHA256: policy.Digest(), ToolVersion: "test"}
	for _, scenario := range []string{"valid", "source", "config", "policy", "version"} {
		t.Run(scenario, func(t *testing.T) {
			changed := receipt
			switch scenario {
			case "source":
				changed.SourceCommit = strings.Repeat("b", 40)
			case "config":
				changed.ConfigSHA256 = strings.Repeat("b", 64)
			case "policy":
				changed.PolicySHA256 = strings.Repeat("b", 64)
			case "version":
				changed.ToolVersion = "other"
			}
			err := validatePublicationReceipt(changed, receipt.SourceCommit, data, policy, "test")
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("receipt binding: %v", err)
			}
		})
	}
}

func TestReconcileRequiresSealIdentityArguments(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	dir, sha := controlFixture(t)
	args := controlArgs(dir, sha)
	args[1] = "reconcile"
	var out, errs bytes.Buffer
	if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 2 {
		t.Fatalf("missing seal identity exit%d %s", exit, errs.String())
	}
	args = append(args, "--source-repository-id", "1", "--run-id", "2", "--artifact-id", "3", "--artifact-digest", strings.Repeat("a", 64))
	for _, name := range []string{"VITRYNA_SOURCE_TOKEN", "VITRYNA_APP_CLIENT_ID", "VITRYNA_APP_PRIVATE_KEY", "GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_REPOSITORY_ID"} {
		t.Setenv(name, "")
	}
	out.Reset()
	errs.Reset()
	if exit := Run(context.Background(), args, &out, &errs, "test"); exit != 1 || !strings.Contains(errs.String(), "publication_credentials_missing") {
		t.Fatalf("missing credentials exit%d %s", exit, errs.String())
	}
}
