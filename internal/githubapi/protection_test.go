package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProtectionReadsAndNativeEnableContract(t *testing.T) {
	var calls []string
	client := New("synthetic", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/repos/example/public/rules/branches/main":
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Fatal("rules pagination missing")
			}
			return response(200, `[{"type":"required_status_checks","ruleset_id":1,"parameters":{"required_status_checks":[{"context":"public-ci"}]}}]`), nil
		case "/repos/example/public/branches/main/protection":
			return response(200, `{"required_status_checks":{"contexts":["ci"]}}`), nil
		case "/graphql":
			var input struct {
				Query     string            `json:"query"`
				Variables map[string]string `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(input.Query, "enablePullRequestAutoMerge") || strings.Contains(input.Query, "mergePullRequest(") || !strings.Contains(input.Query, "expectedHeadOid:$head") || input.Variables["head"] != strings.Repeat("a", 40) || input.Variables["id"] != "PR_fixture" || input.Variables["method"] != "SQUASH" {
				t.Fatal("not an explicit head-bound native auto-merge request")
			}
			return response(200, `{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"id":"PR_fixture","merged":false,"autoMergeRequest":{"mergeMethod":"SQUASH"}}}}}`), nil
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
			return nil, nil
		}
	})})
	rules, err := client.BranchRules(context.Background(), "example/public", "main")
	if err != nil || len(rules) != 1 || rules[0].Type != "required_status_checks" {
		t.Fatalf("rules: %#v %v", rules, err)
	}
	classic, err := client.ClassicProtection(context.Background(), "example/public", "main")
	if err != nil || len(classic) == 0 {
		t.Fatalf("classic: %s %v", classic, err)
	}
	if err := client.EnableAutoMerge(context.Background(), "PR_fixture", "squash", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("unexpected requests %v", calls)
	}
}

func TestNativeEnableRequiresExactConfirmation(t *testing.T) {
	for _, scenario := range []string{"wrong node", "wrong method", "missing request", "partial error", "merged"} {
		t.Run(scenario, func(t *testing.T) {
			body := `{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"id":"PR_fixture","merged":false,"autoMergeRequest":{"mergeMethod":"SQUASH"}}}}}`
			switch scenario {
			case "wrong node":
				body = strings.ReplaceAll(body, "PR_fixture", "other")
			case "wrong method":
				body = strings.ReplaceAll(body, "SQUASH", "MERGE")
			case "missing request":
				body = `{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"id":"PR_fixture","merged":false,"autoMergeRequest":null}}}}`
			case "partial error":
				body = `{"data":{},"errors":[{"message":"synthetic secret"}]}`
			case "merged":
				body = `{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"id":"PR_fixture","merged":true,"autoMergeRequest":null}}}}`
			}
			client := New("", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })})
			err := client.EnableAutoMerge(context.Background(), "PR_fixture", "squash", strings.Repeat("a", 40))
			if scenario == "merged" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "synthetic secret") {
				t.Fatalf("unsafe confirmation %v", err)
			}
		})
	}
}
