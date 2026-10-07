package githubapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactProvenanceAndUnauthenticatedRedirect(t *testing.T) {
	body := "synthetic-provider-bytes"
	hash := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(hash[:])
	for _, scenario := range []string{"valid", "explicit preview fork", "unexpected fork", "wrong id", "wrong run", "wrong source", "wrong repository", "expired", "missing digest", "wrong digest", "raw tamper"} {
		t.Run(scenario, func(t *testing.T) {
			var calls []string
			client := New("synthetic-source-token", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Host+r.URL.Path)
				if r.URL.Host == "storage.example.invalid" {
					if r.Header.Get("Authorization") != "" {
						t.Fatal("source Authorization reached redirected storage")
					}
					if scenario == "raw tamper" {
						return response(200, "changed-provider-bytes"), nil
					}
					return response(200, body), nil
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-source-token" {
					t.Fatal("API source token missing")
				}
				if strings.HasSuffix(r.URL.Path, "/zip") {
					out := response(302, "")
					out.Header.Set("Location", "https://storage.example.invalid/bundle?signature=private-signed-value")
					return out, nil
				}
				id, run, repo := int64(11), int64(22), int64(33)
				headRepo := repo
				if scenario == "explicit preview fork" || scenario == "unexpected fork" {
					headRepo = 44
				}
				sha := strings.Repeat("a", 40)
				expired := false
				mdigest := "sha256:" + digest
				switch scenario {
				case "wrong id":
					id++
				case "wrong run":
					run++
				case "wrong source":
					sha = strings.Repeat("b", 40)
				case "wrong repository":
					repo++
				case "expired":
					expired = true
				case "missing digest":
					mdigest = ""
				case "wrong digest":
					mdigest = "sha256:" + strings.Repeat("0", 64)
				}
				return response(200, fmt.Sprintf(`{"id":%d,"size_in_bytes":%d,"expired":%t,"expires_at":%q,"digest":%q,"workflow_run":{"id":%d,"repository_id":%d,"head_repository_id":%d,"head_sha":%q}}`, id, len(body), expired, time.Now().Add(time.Hour).UTC().Format(time.RFC3339), mdigest, run, repo, headRepo, sha)), nil
			})})
			dest := filepath.Join(t.TempDir(), "provider.zip")
			expectation := ArtifactExpectation{Repository: "example/source", RepositoryID: 33, RunID: 22, ArtifactID: 11, SourceCommit: strings.Repeat("a", 40), Digest: digest}
			if scenario == "explicit preview fork" {
				expectation.HeadRepositoryID = 44
			}
			err := client.DownloadArtifact(context.Background(), expectation, dest)
			if scenario == "valid" || scenario == "explicit preview fork" {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(dest)
				if err != nil || string(data) != body {
					t.Fatalf("download mismatch: %v", err)
				}
				if len(calls) != 3 {
					t.Fatalf("unexpected call trace: %v", calls)
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "private-signed-value") || strings.Contains(err.Error(), "synthetic-source-token") {
					t.Fatalf("unsafe download outcome: %v", err)
				}
				if _, err := os.Lstat(dest); !os.IsNotExist(err) {
					t.Fatal("failed download exposed archive")
				}
				if scenario != "raw tamper" && len(calls) != 1 {
					t.Fatalf("download began before provenance validation: %v", calls)
				}
			}
		})
	}
}
