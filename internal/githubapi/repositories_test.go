package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPublicPullCreateAndReopenPayload(t *testing.T) {
	for _, operation := range []string{"create", "reopen"} {
		t.Run(operation, func(t *testing.T) {
			client := New("synthetic", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if operation == "create" {
					if r.Method != "POST" || r.URL.Path != "/repos/example/public/pulls" || len(body) != 4 || body["base"] != "main" || body["head"] != "vitryna/public" || body["title"] != "Update public repository" || body["body"] != "Public repository update.\n\n<!-- vitryna-publish:v1 -->" {
						t.Fatal("unexpected create request")
					}
				} else if r.Method != "PATCH" || r.URL.Path != "/repos/example/public/pulls/11" || len(body) != 1 || body["state"] != "open" {
					t.Fatal("unexpected reopen request")
				}
				return response(200, `{"number":11,"state":"open"}`), nil
			})})
			var pull PullRequest
			var err error
			if operation == "create" {
				pull, err = client.CreatePullRequest(context.Background(), "example/public", "main", "vitryna/public")
			} else {
				pull, err = client.ReopenPullRequest(context.Background(), "example/public", 11)
			}
			if err != nil || pull.Number != 11 {
				t.Fatalf("pull mutation: %#v %v", pull, err)
			}
		})
	}
}

func TestExistingWorkflowsRequireWriteScope(t *testing.T) {
	for _, scenario := range []string{"absent", "present", "empty", "wrong path", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			client := New("", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" || r.URL.Path != "/repos/example/public/contents/.github/workflows" || r.URL.Query().Get("ref") != strings.Repeat("a", 40) {
					t.Fatal("workflow scope query not public and immutable")
				}
				switch scenario {
				case "absent":
					return response(404, "{}"), nil
				case "empty":
					return response(200, "[]"), nil
				case "wrong path":
					return response(200, `[{"path":"outside.yml"}]`), nil
				case "unavailable":
					return response(403, "{}"), nil
				default:
					return response(200, `[{"path":".github/workflows/check.yml"}]`), nil
				}
			})})
			has, err := client.HasWorkflows(context.Background(), "example/public", strings.Repeat("a", 40))
			if scenario == "wrong path" || scenario == "unavailable" {
				if err == nil {
					t.Fatal("ambiguous workflow permissions accepted")
				}
				return
			}
			if err != nil || has != (scenario == "present") {
				t.Fatalf("scope %v %v", has, err)
			}
		})
	}
}

func TestRefExactIdentityAndNotFound(t *testing.T) {
	for _, scenario := range []string{"valid", "absent", "wrong ref", "wrong type", "invalid sha"} {
		t.Run(scenario, func(t *testing.T) {
			client := New("fake", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/repos/example/public/git/ref/heads/feature/publish" {
					t.Fatalf("unexpected ref path: %s", r.URL.Path)
				}
				if scenario == "absent" {
					return response(404, `{"message":"missing"}`), nil
				}
				ref, kind, sha := "refs/heads/feature/publish", "commit", strings.Repeat("a", 40)
				if scenario == "wrong ref" {
					ref = "refs/heads/main"
				}
				if scenario == "wrong type" {
					kind = "tag"
				}
				if scenario == "invalid sha" {
					sha = "invalid"
				}
				return response(200, `{"ref":"`+ref+`","object":{"type":"`+kind+`","sha":"`+sha+`"}}`), nil
			})})
			sha, err := client.Ref(context.Background(), "example/public", "feature/publish")
			if scenario == "valid" {
				if err != nil || sha != strings.Repeat("a", 40) {
					t.Fatalf("validref %q %v", sha, err)
				}
			} else if scenario == "absent" {
				if err != nil || sha != "" {
					t.Fatalf("absentref %q %v", sha, err)
				}
			} else if err == nil {
				t.Fatal("invalid ref accepted")
			}
		})
	}
}

func TestGraphQLRefusesPartialErrorsAndConfirmsDisarm(t *testing.T) {
	for _, scenario := range []string{"valid", "errors", "still armed", "wrong node"} {
		t.Run(scenario, func(t *testing.T) {
			client := New("fake", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/graphql" || r.Method != "POST" {
					t.Fatal("unexpected GraphQL request")
				}
				body := `{"data":{"disablePullRequestAutoMerge":{"pullRequest":{"id":"PR_fixture","autoMergeRequest":null}}}}`
				if scenario == "errors" {
					body = `{"data":{},"errors":[{"message":"private failure"}]}`
				}
				if scenario == "still armed" {
					body = `{"data":{"disablePullRequestAutoMerge":{"pullRequest":{"id":"PR_fixture","autoMergeRequest":{"enabledAt":"2026-01-01"}}}}}`
				}
				if scenario == "wrong node" {
					body = strings.ReplaceAll(body, "PR_fixture", "other")
				}
				return response(200, body), nil
			})})
			err := client.DisableAutoMerge(context.Background(), "PR_fixture")
			if scenario == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || strings.Contains(err.Error(), "private failure") {
				t.Fatalf("unsafe GraphQL response: %v", err)
			}
		})
	}
}
