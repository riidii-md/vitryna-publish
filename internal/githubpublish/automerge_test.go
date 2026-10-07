package githubpublish

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type autoAPI struct {
	*fakeAPI
	rules                                  []githubapi.BranchRule
	classic                                json.RawMessage
	rulesErr, classicErr, enableErr        error
	enableApplied, mergeBeforeInvalidation bool
	onEnable                               func()
}

func (f *autoAPI) BranchRules(context.Context, string, string) ([]githubapi.BranchRule, error) {
	f.calls = append(f.calls, "rules")
	return f.rules, f.rulesErr
}
func (f *autoAPI) ClassicProtection(context.Context, string, string) (json.RawMessage, error) {
	f.calls = append(f.calls, "classic")
	return f.classic, f.classicErr
}
func (f *autoAPI) EnableAutoMerge(_ context.Context, node, method, head string) error {
	f.calls = append(f.calls, "enable")
	if f.onEnable != nil {
		f.onEnable()
	}
	if f.enableErr == nil || f.enableApplied {
		for i := range f.pulls {
			if f.pulls[i].NodeID == node && f.pulls[i].Head.SHA == head {
				value, _ := json.Marshal(map[string]any{"merge_method": method, "enabled_by": map[string]any{"id": 7}})
				raw := json.RawMessage(value)
				f.pulls[i].AutoMerge = &raw
			}
		}
	}
	return f.enableErr
}

func rule(kind, parameters string) githubapi.BranchRule {
	return githubapi.BranchRule{Type: kind, RulesetID: 1, Parameters: json.RawMessage(parameters)}
}

func autoFixture() (*Publisher, *autoAPI) {
	p, f := ownedFixture()
	p.Config.Merge = config.Merge{Mode: "auto", Method: "squash", ProtectionInspection: "effective_rulesets"}
	f.repository.AllowAutoMerge, f.repository.AllowSquashMerge = true, true
	f.pulls[0].AutoMerge = nil
	a := &autoAPI{fakeAPI: f, rules: []githubapi.BranchRule{rule("required_status_checks", `{"required_status_checks":[{"context":"public-ci"}]}`)}}
	p.API = a
	return p, a
}

func TestAutoMergeRequiresBlockingFreshProtection(t *testing.T) {
	for _, scenario := range []string{"checks", "fresh review", "last push review", "queue", "no gates", "routing only", "empty checks", "stale review", "code owner stale", "conditional review stale", "malformed review", "unknown method", "repository disabled", "method disabled", "rules unavailable", "classic checks", "classic unavailable", "classic stale review", "classic plus stale rules"} {
		t.Run(scenario, func(t *testing.T) {
			p, a := autoFixture()
			allowed := false
			switch scenario {
			case "checks":
				allowed = true
			case "fresh review":
				allowed = true
				a.rules = []githubapi.BranchRule{rule("pull_request", `{"required_approving_review_count":1,"dismiss_stale_reviews_on_push":true,"require_last_push_approval":false}`)}
			case "last push review":
				allowed = true
				a.rules = []githubapi.BranchRule{rule("pull_request", `{"required_approving_review_count":1,"dismiss_stale_reviews_on_push":false,"require_last_push_approval":true}`)}
			case "queue":
				allowed = true
				a.rules = []githubapi.BranchRule{rule("merge_queue", `{"merge_method":"SQUASH"}`)}
			case "no gates":
				a.rules = nil
			case "routing only":
				a.rules = []githubapi.BranchRule{rule("pull_request", `{"required_approving_review_count":0,"dismiss_stale_reviews_on_push":false,"require_last_push_approval":false}`)}
			case "empty checks":
				a.rules = []githubapi.BranchRule{rule("required_status_checks", `{"required_status_checks":[]}`)}
			case "stale review":
				a.rules = append(a.rules, rule("pull_request", `{"required_approving_review_count":1,"dismiss_stale_reviews_on_push":false,"require_last_push_approval":false}`))
			case "code owner stale":
				a.rules = append(a.rules, rule("pull_request", `{"required_approving_review_count":0,"require_code_owner_review":true,"dismiss_stale_reviews_on_push":false,"require_last_push_approval":false}`))
			case "conditional review stale":
				a.rules = append(a.rules, rule("pull_request", `{"required_approving_review_count":0,"required_reviewers":[{"minimum_approvals":1}],"dismiss_stale_reviews_on_push":false,"require_last_push_approval":false}`))
			case "malformed review":
				a.rules = append(a.rules, rule("pull_request", `{"required_approving_review_count":"1"}`))
			case "unknown method":
				p.Config.Merge.Method = "fast"
			case "repository disabled":
				a.repository.AllowAutoMerge = false
			case "method disabled":
				a.repository.AllowSquashMerge = false
			case "rules unavailable":
				a.rulesErr = &githubapi.Error{Code: "github_http_failed", Status: 403}
			case "classic checks":
				allowed = true
				p.Config.Merge.ProtectionInspection = "classic"
				a.rules = []githubapi.BranchRule{}
				a.classic = json.RawMessage(`{"required_status_checks":{"contexts":["ci"]}}`)
			case "classic unavailable":
				p.Config.Merge.ProtectionInspection = "classic"
				a.classicErr = &githubapi.Error{Code: "github_http_failed", Status: 403}
			case "classic stale review":
				p.Config.Merge.ProtectionInspection = "classic"
				a.classic = json.RawMessage(`{"required_pull_request_reviews":{"required_approving_review_count":1,"dismiss_stale_reviews":false,"require_last_push_approval":false}}`)
			case "classic plus stale rules":
				p.Config.Merge.ProtectionInspection = "classic"
				a.classic = json.RawMessage(`{"required_status_checks":{"contexts":["ci"]}}`)
				a.rules = append(a.rules, rule("pull_request", `{"required_approving_review_count":1,"dismiss_stale_reviews_on_push":false,"require_last_push_approval":false}`))
			}
			err := p.AutoPreflight(context.Background())
			if (err == nil) != allowed {
				t.Fatalf("preflight allowed=%v err=%v", allowed, err)
			}
			for _, call := range a.calls {
				if call == "enable" || call == "close" || call == "disable" {
					t.Fatal("preflight mutated target")
				}
				if p.Config.Merge.ProtectionInspection != "classic" && call == "classic" {
					t.Fatal("ruleset mode queried administration endpoint")
				}
			}
		})
	}
}

func TestManualDoesNotInspectProtection(t *testing.T) {
	p, a := autoFixture()
	p.Config.Merge = config.Merge{Mode: "manual"}
	if err := p.AutoPreflight(context.Background()); err != nil || len(a.calls) != 0 {
		t.Fatalf("manual protection request %v %v", a.calls, err)
	}
}

func TestPreflightDoesNotInvalidateExistingCandidate(t *testing.T) {
	p, a := autoFixture()
	result, err := p.Preflight(context.Background())
	if err != nil || result.Status != "preflight_ready" {
		t.Fatalf("preflight %#v %v", result, err)
	}
	for _, call := range a.calls {
		if call == "close" || call == "disable" || call == "enable" {
			t.Fatal("preflight mutated candidate")
		}
	}
	if a.pulls[0].State != "open" {
		t.Fatal("preflight closed PR")
	}
}

func TestNativeAutoMergeConfirmsStateAndRefusesStale(t *testing.T) {
	for _, scenario := range []string{"enabled", "stale", "rejected", "ambiguous applied", "ambiguous unapplied", "already merged"} {
		t.Run(scenario, func(t *testing.T) {
			p, a := autoFixture()
			expected := a.pulls[0]
			switch scenario {
			case "stale":
				p.Fresh = func(context.Context) error { return errors.New("stale") }
			case "rejected":
				a.enableErr = &githubapi.Error{Code: "github_graphql_failed"}
			case "ambiguous applied":
				a.enableErr = &githubapi.Error{Code: "github_ambiguous_write"}
				a.enableApplied = true
			case "ambiguous unapplied":
				a.enableErr = &githubapi.Error{Code: "github_ambiguous_write"}
			case "already merged":
				a.pulls[0].State = "closed"
				a.pulls[0].Merged = true
			}
			result, err := p.EnableNativeAutoMerge(context.Background(), expected)
			allowed := scenario == "enabled" || scenario == "ambiguous applied" || scenario == "already merged"
			if (err == nil) != allowed {
				t.Fatalf("native result %#v %v", result, err)
			}
			enables := 0
			for _, call := range a.calls {
				if call == "enable" {
					enables++
				}
			}
			if (scenario == "stale" || scenario == "already merged") && enables != 0 {
				t.Fatal("stale/merged PR sent enable mutation")
			}
			if scenario == "ambiguous applied" && enables != 1 || scenario == "ambiguous unapplied" && enables != 2 {
				t.Fatalf("unexpected retries %d", enables)
			}
			if !allowed && a.pulls[0].State != "open" {
				t.Fatal("denied auto-merge closed reviewable PR")
			}
		})
	}
}
