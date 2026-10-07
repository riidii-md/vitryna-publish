package githubpublish

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type reconcileAutoAPI struct {
	*localAPI
	auto    *autoAPI
	onRules func()
	t       *testing.T
}

func (f *reconcileAutoAPI) BranchRules(ctx context.Context, repo, branch string) ([]githubapi.BranchRule, error) {
	if f.onRules != nil {
		f.onRules()
	}
	return f.auto.BranchRules(ctx, repo, branch)
}
func (f *reconcileAutoAPI) ClassicProtection(ctx context.Context, repo, branch string) (json.RawMessage, error) {
	return f.auto.ClassicProtection(ctx, repo, branch)
}
func (f *reconcileAutoAPI) EnableAutoMerge(ctx context.Context, node, method, head string) error {
	actual := runGit(f.t, f.remote, "rev-parse", "refs/heads/vitryna/public")
	if actual != head || len(f.pulls) != 1 || f.pulls[0].NodeID != node || f.pulls[0].Head.SHA != head {
		f.t.Fatalf("native handoff is not bound to real pushed head/new PR: node=%s head=%s actual=%s", node, head, actual)
	}
	return f.auto.EnableAutoMerge(ctx, node, method, head)
}

func TestReconcileNativeAutoBoundary(t *testing.T) {
	for _, scenario := range []string{"allowed", "denied protection", "rejected mutation", "stale after open"} {
		t.Run(scenario, func(t *testing.T) {
			remote, base, tree, receipt := targetFixture(t)
			p, f := ownedFixture()
			f.pulls, f.head = nil, ""
			f.repository.AllowAutoMerge, f.repository.AllowSquashMerge = true, true
			p.Config.Merge = config.Merge{Mode: "auto", Method: "squash", ProtectionInspection: "effective_rulesets"}
			api := &reconcileAutoAPI{localAPI: &localAPI{fakeAPI: f, remote: remote, nextNumber: 10}, auto: &autoAPI{fakeAPI: f, rules: []githubapi.BranchRule{rule("required_status_checks", `{"required_status_checks":[{"context":"public-ci"}]}`)}}, t: t}
			p.API = api
			stale := false
			p.Fresh = func(context.Context) error {
				if stale {
					return errors.New("synthetic source stale")
				}
				return nil
			}
			switch scenario {
			case "denied protection":
				api.auto.rules = nil
			case "rejected mutation":
				api.auto.enableErr = &githubapi.Error{Code: "github_graphql_failed"}
			case "stale after open":
				api.onRules = func() { stale = true }
			}
			result, err := p.Reconcile(context.Background(), ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"})
			allowed := scenario == "allowed"
			wantStatus := "pull_open_auto_denied"
			if allowed {
				wantStatus = "auto_merge_enabled"
			}
			if (err == nil) != allowed || result.Status != wantStatus || result.PullNumber != 11 {
				t.Fatalf("composed native result: scenario=%s result=%#v err=%v", scenario, result, err)
			}
			head := runGit(t, remote, "rev-parse", "refs/heads/vitryna/public")
			if runGit(t, remote, "rev-list", "--parents", "-n", "1", head) != head+" "+base || runGit(t, remote, "rev-parse", "refs/heads/main") != base {
				t.Fatal("auto path mutated public base or lost public-only ancestry")
			}
			if len(api.pulls) != 1 || api.pulls[0].Number != 11 || api.pulls[0].State != "open" || api.pulls[0].Head.SHA != head || (api.pulls[0].AutoMerge != nil) != allowed {
				t.Fatalf("new PR/head/armed state mismatch: %#v", api.pulls)
			}
			if !p.ownedPull(api.pulls[0], api.repository) {
				t.Fatal("auto path created an unowned candidate")
			}
			if allowed {
				var armed struct {
					Method string `json:"merge_method"`
					Actor  struct {
						ID int64 `json:"id"`
					} `json:"enabled_by"`
				}
				if err := json.Unmarshal(*api.pulls[0].AutoMerge, &armed); err != nil || armed.Method != "squash" || armed.Actor.ID != 7 {
					t.Fatalf("wrong native method or App actor: %#v %v", armed, err)
				}
			}
			enables := 0
			for _, call := range api.calls {
				if call == "enable" {
					enables++
				}
			}
			wantEnables := 0
			if allowed || scenario == "rejected mutation" {
				wantEnables = 1
			}
			if enables != wantEnables {
				t.Fatalf("unexpected enable requests: %d want %d", enables, wantEnables)
			}
		})
	}
}
