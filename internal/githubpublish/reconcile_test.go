package githubpublish

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type localAPI struct {
	*fakeAPI
	remote            string
	reopenUnavailable bool
	nextNumber        int
	createError       error
	createApplied     bool
	onRef             func(string)
	onPullRead        func()
}

func (f *localAPI) Ref(ctx context.Context, _ string, branch string) (string, error) {
	if f.onRef != nil {
		f.onRef(branch)
	}
	f.calls = append(f.calls, "ref:"+branch)
	data, err := exec.CommandContext(ctx, "git", "-C", f.remote, "rev-parse", "--verify", "refs/heads/"+branch).Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(data)), nil
}
func (f *localAPI) Commit(ctx context.Context, _ string, oid string) (githubapi.Commit, error) {
	f.calls = append(f.calls, "commit")
	data, err := exec.CommandContext(ctx, "git", "-C", f.remote, "show", "-s", "--format=%H%n%ae%n%ce%n%B", oid).Output()
	if err != nil {
		return githubapi.Commit{}, err
	}
	lines := strings.SplitN(string(data), "\n", 4)
	c := githubapi.Commit{SHA: lines[0]}
	c.Commit.Message = lines[3]
	if lines[1] == "7+fixture[bot]@users.noreply.github.com" {
		c.Author.ID = 7
	}
	if lines[2] == "7+fixture[bot]@users.noreply.github.com" {
		c.Committer.ID = 7
	}
	return c, nil
}
func (f *localAPI) updateHeads(ctx context.Context) {
	head, _ := f.Ref(ctx, "", "vitryna/public")
	for i := range f.pulls {
		if head != "" && f.pulls[i].MergedAt == nil {
			f.pulls[i].Head.SHA = head
		}
	}
}
func (f *localAPI) WalkPullRequests(ctx context.Context, repository, base, head string, visit func(githubapi.PullRequest) error) error {
	f.updateHeads(ctx)
	return f.fakeAPI.WalkPullRequests(ctx, repository, base, head, visit)
}
func (f *localAPI) PullRequest(ctx context.Context, repository string, number int) (githubapi.PullRequest, error) {
	f.updateHeads(ctx)
	if f.onPullRead != nil {
		f.onPullRead()
	}
	return f.fakeAPI.PullRequest(ctx, repository, number)
}
func (f *localAPI) CreatePullRequest(ctx context.Context, repository, base, head string) (githubapi.PullRequest, error) {
	f.calls = append(f.calls, "create")
	if f.createError != nil && !f.createApplied {
		return githubapi.PullRequest{}, f.createError
	}
	sha, _ := f.Ref(ctx, repository, head)
	f.nextNumber++
	pull := githubapi.PullRequest{Number: f.nextNumber, NodeID: "PR_local_" + sha, State: "open", User: githubapi.User{ID: 7}, Body: "Public repository update.\n\n<!-- vitryna-publish:v1 -->", Head: githubapi.PullRef{Ref: head, SHA: sha, Repo: f.repository}, Base: githubapi.PullRef{Ref: base, Repo: f.repository}}
	f.pulls = append(f.pulls, pull)
	if f.createError != nil {
		return githubapi.PullRequest{}, f.createError
	}
	return pull, nil
}
func (f *localAPI) ReopenPullRequest(ctx context.Context, repository string, number int) (githubapi.PullRequest, error) {
	f.calls = append(f.calls, "reopen")
	if f.reopenUnavailable {
		return githubapi.PullRequest{}, &githubapi.Error{Code: "github_http_failed", Status: 422}
	}
	for i := range f.pulls {
		if f.pulls[i].Number == number {
			f.pulls[i].State = "open"
		}
	}
	return f.PullRequest(ctx, repository, number)
}

func TestManualReconcileAndNoDiff(t *testing.T) {
	remote, base, tree, receipt := targetFixture(t)
	p, original := ownedFixture()
	original.pulls = nil
	original.head = ""
	api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10}
	p.API = api
	request := ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"}
	result, err := p.Reconcile(context.Background(), request)
	if err != nil || result.Status != "pull_open" || result.PullNumber != 11 {
		t.Fatalf("reconcile: %#v %v", result, err)
	}
	head := runGit(t, remote, "rev-parse", "refs/heads/vitryna/public")
	if got := runGit(t, remote, "rev-list", "--parents", "-n", "1", head); got != head+" "+base {
		t.Fatal("reconciler changed public ancestry")
	}
	if len(api.pulls) != 1 || api.pulls[0].AutoMerge != nil || strings.Contains(api.pulls[0].Body, receipt.SourceCommit) {
		t.Fatal("manual PR leaked provenance or armed auto-merge")
	}
	result, err = p.Reconcile(context.Background(), request)
	if err != nil || result.PullNumber != 11 || len(api.pulls) != 1 {
		t.Fatalf("owned PR not reopened idempotently: %#v %v", result, err)
	}
	head = runGit(t, remote, "rev-parse", "refs/heads/vitryna/public")
	runGit(t, remote, "update-ref", "refs/heads/main", head, base)
	result, err = p.Reconcile(context.Background(), request)
	if err != nil || result.Status != "no_diff" {
		t.Fatalf("no-diff: %#v %v", result, err)
	}
	if api.pulls[0].State != "closed" || api.pulls[0].AutoMerge != nil {
		t.Fatal("no-diff candidate not closed/disarmed")
	}
	if err := exec.Command("git", "-C", remote, "rev-parse", "--verify", "refs/heads/vitryna/public").Run(); err == nil {
		t.Fatal("no-diff promotion ref retained")
	}
	if got := runGit(t, remote, "rev-parse", "refs/heads/main"); got != head {
		t.Fatal("no-diff deleted/changed base")
	}
}

func TestReopenUnavailableCreatesNewOwnedPull(t *testing.T) {
	remote, _, tree, receipt := targetFixture(t)
	p, original := ownedFixture()
	original.pulls = nil
	original.head = ""
	api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10}
	p.API = api
	request := ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"}
	if _, err := p.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	api.reopenUnavailable = true
	result, err := p.Reconcile(context.Background(), request)
	if err != nil || result.PullNumber != 12 || len(api.pulls) != 2 || api.pulls[0].State != "closed" {
		t.Fatalf("fallback: %#v %v", result, err)
	}
}

func TestReconcileRechecksFreshnessBeforeReopen(t *testing.T) {
	remote, _, tree, receipt := targetFixture(t)
	p, original := ownedFixture()
	original.pulls, original.head = nil, ""
	api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10}
	p.API = api
	request := ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"}
	if _, err := p.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	stale := false
	baseReads := 0
	api.onRef = func(branch string) {
		if branch == "main" {
			baseReads++
		}
	}
	p.Fresh = func(context.Context) error {
		if stale {
			return errors.New("stale fixture")
		}
		return nil
	}
	api.onPullRead = func() {
		if baseReads >= 2 && api.pulls[0].State == "closed" {
			stale = true
		}
	}
	api.calls = nil
	if _, err := p.Reconcile(context.Background(), request); err == nil {
		t.Fatal("stale source reopened PR")
	}
	for _, call := range api.calls {
		if call == "reopen" || call == "create" {
			t.Fatal("stale source caused PR mutation")
		}
	}
	if api.pulls[0].State != "closed" {
		t.Fatal("candidate was not left closed")
	}
}

func TestReconcileUncertainCreateObservesBeforeRetry(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(map[bool]string{false: "not applied", true: "applied"}[applied], func(t *testing.T) {
			remote, _, tree, receipt := targetFixture(t)
			p, original := ownedFixture()
			original.pulls, original.head = nil, ""
			api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10, createError: &githubapi.Error{Code: "github_ambiguous_write"}, createApplied: applied}
			p.API = api
			result, err := p.Reconcile(context.Background(), ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"})
			creates := 0
			for i, call := range api.calls {
				if call == "create" {
					creates++
					if creates > 1 {
						observed := false
						for j := i - 1; j >= 0 && api.calls[j] != "create"; j-- {
							if api.calls[j] == "pulls" {
								observed = true
							}
						}
						if !observed {
							t.Fatal("blind retry")
						}
					}
				}
			}
			if applied {
				if err != nil || result.PullNumber != 11 || creates != 1 || len(api.pulls) != 1 {
					t.Fatalf("applied response: %#v %v calls=%v", result, err, api.calls)
				}
			} else if err == nil || creates != 3 || len(api.pulls) != 0 {
				t.Fatalf("unapplied response: %#v %v creates=%d", result, err, creates)
			}
		})
	}
}

func TestReconcileLastMinuteDefaultGuard(t *testing.T) {
	remote, base, tree, receipt := targetFixture(t)
	p, original := ownedFixture()
	original.pulls, original.head = nil, ""
	api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10}
	p.API = api
	baseReads := 0
	api.onRef = func(branch string) {
		if branch == "main" {
			baseReads++
			if baseReads == 2 {
				original.repository.DefaultBranch = "vitryna/public"
			}
		}
	}
	if _, err := p.Reconcile(context.Background(), ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"}); err == nil {
		t.Fatal("reassigned default was mutated")
	}
	if got := runGit(t, remote, "rev-parse", "main"); got != base {
		t.Fatal("public base changed")
	}
	if err := exec.Command("git", "-C", remote, "rev-parse", "--verify", "refs/heads/vitryna/public").Run(); err == nil {
		t.Fatal("reserved promotion created")
	}
}

func TestReconcileRebuildsMovedBaseButBoundsChurn(t *testing.T) {
	for _, churn := range []bool{false, true} {
		t.Run(map[bool]string{false: "one movement", true: "continuous movement"}[churn], func(t *testing.T) {
			remote, base, tree, receipt := targetFixture(t)
			p, original := ownedFixture()
			original.pulls, original.head = nil, ""
			api := &localAPI{fakeAPI: original, remote: remote, nextNumber: 10}
			p.API = api
			baseReads := 0
			api.onRef = func(branch string) {
				if branch != "main" {
					return
				}
				baseReads++
				if baseReads == 2 || churn && baseReads == 4 {
					root := runGit(t, remote, "rev-parse", base+"^{tree}")
					next := runGit(t, remote, "-c", "user.name=Public", "-c", "user.email=public@example.invalid", "commit-tree", root, "-p", base, "-m", "Concurrent public change")
					runGit(t, remote, "update-ref", "refs/heads/main", next, base)
					base = next
				}
			}
			result, err := p.Reconcile(context.Background(), ReconcileRequest{Directory: tree, Receipt: receipt, Remote: remote, BotLogin: "fixture[bot]"})
			if churn {
				if !errors.Is(err, ErrBaseMoved) || len(api.pulls) != 0 {
					t.Fatalf("churn not bounded: %#v %v", result, err)
				}
				if err := exec.Command("git", "-C", remote, "rev-parse", "--verify", "refs/heads/vitryna/public").Run(); err == nil {
					t.Fatal("churn published a branch")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				head := runGit(t, remote, "rev-parse", "refs/heads/vitryna/public")
				if parent := runGit(t, remote, "rev-parse", head+"^"); parent != base {
					t.Fatal("rebuild ignored latest base")
				}
			}
		})
	}
}
