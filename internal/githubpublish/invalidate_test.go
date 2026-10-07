package githubpublish

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type fakeAPI struct {
	repository   githubapi.Repository
	head         string
	commit       githubapi.Commit
	pulls        []githubapi.PullRequest
	calls        []string
	closeError   error
	disableError error
	onRepository func()
}

func (f *fakeAPI) Repository(context.Context, string) (githubapi.Repository, error) {
	f.calls = append(f.calls, "repository")
	if f.onRepository != nil {
		f.onRepository()
	}
	return f.repository, nil
}
func (f *fakeAPI) Ref(context.Context, string, string) (string, error) {
	f.calls = append(f.calls, "ref")
	return f.head, nil
}
func (f *fakeAPI) Commit(context.Context, string, string) (githubapi.Commit, error) {
	f.calls = append(f.calls, "commit")
	return f.commit, nil
}
func (f *fakeAPI) WalkPullRequests(_ context.Context, _ string, _ string, _ string, visit func(githubapi.PullRequest) error) error {
	f.calls = append(f.calls, "pulls")
	for _, pull := range f.pulls {
		if err := visit(pull); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeAPI) PullRequest(_ context.Context, _ string, number int) (githubapi.PullRequest, error) {
	f.calls = append(f.calls, "pull")
	for _, p := range f.pulls {
		if p.Number == number {
			return p, nil
		}
	}
	return githubapi.PullRequest{}, errors.New("missing fixture pull")
}
func (f *fakeAPI) DisableAutoMerge(_ context.Context, nodeID string) error {
	f.calls = append(f.calls, "disable")
	if f.disableError != nil {
		return f.disableError
	}
	for i := range f.pulls {
		if f.pulls[i].NodeID == nodeID {
			f.pulls[i].AutoMerge = nil
		}
	}
	return nil
}
func (f *fakeAPI) ClosePullRequest(_ context.Context, _ string, number int) error {
	f.calls = append(f.calls, "close")
	if f.closeError != nil {
		return f.closeError
	}
	for i := range f.pulls {
		if f.pulls[i].Number == number {
			f.pulls[i].State = "closed"
		}
	}
	return nil
}

func ownedFixture() (*Publisher, *fakeAPI) {
	private := false
	f := &fakeAPI{repository: githubapi.Repository{ID: 1, FullName: "example/public", Private: &private, DefaultBranch: "main"}, head: strings.Repeat("a", 40)}
	f.commit.SHA = f.head
	f.commit.Author.ID = 7
	f.commit.Committer.ID = 7
	f.commit.Commit.Message = "Update public repository\n\nVitryna-Publish: v1\n"
	armed := json.RawMessage(`{"enabled_by":{"id":7}}`)
	f.pulls = []githubapi.PullRequest{{Number: 3, NodeID: "PR_fixture", State: "open", User: githubapi.User{ID: 7}, Body: "Public repository update.\n\n<!-- vitryna-publish:v1 -->", Head: githubapi.PullRef{Ref: "vitryna/public", SHA: f.head, Repo: f.repository}, Base: githubapi.PullRef{Ref: "main", Repo: f.repository}, AutoMerge: &armed}}
	p := &Publisher{Config: config.GitHub{Repository: "example/public", BaseBranch: "main", PromotionBranch: "vitryna/public", TrustedSourceBranch: "main"}, API: f, BotID: 7, Fresh: func(context.Context) error { return nil }}
	return p, f
}

func mutations(calls []string) []string {
	var result []string
	for _, c := range calls {
		if c == "disable" || c == "close" {
			result = append(result, c)
		}
	}
	return result
}

func TestInvalidateDisarmsBeforeClose(t *testing.T) {
	p, f := ownedFixture()
	result, err := p.Invalidate(context.Background())
	if err != nil || result.PullNumber != 3 || result.Status != "closed" {
		t.Fatalf("invalidation: %#v %v", result, err)
	}
	if !reflect.DeepEqual(mutations(f.calls), []string{"disable", "close"}) || f.pulls[0].AutoMerge != nil || f.pulls[0].State != "closed" {
		t.Fatalf("unsafe ordering: %v", f.calls)
	}
	f.calls = nil
	if _, err := p.Invalidate(context.Background()); err != nil || len(mutations(f.calls)) != 0 {
		t.Fatalf("repeated invalidation not no-op: %v %v", err, f.calls)
	}
}

func TestInvalidateRefusalsDoNotMutate(t *testing.T) {
	for _, scenario := range []string{"stale", "reserved default", "reserved base", "unavailable metadata", "private target", "foreign actor", "foreign marker", "foreign commit", "different head", "multiple open", "foreign head repository"} {
		t.Run(scenario, func(t *testing.T) {
			p, f := ownedFixture()
			switch scenario {
			case "stale":
				p.Fresh = func(context.Context) error { return errors.New("stale") }
			case "reserved default":
				f.repository.DefaultBranch = p.Config.PromotionBranch
			case "reserved base":
				p.Config.PromotionBranch = p.Config.BaseBranch
			case "unavailable metadata":
				f.repository.DefaultBranch = ""
			case "private target":
				private := true
				f.repository.Private = &private
			case "foreign actor":
				f.pulls[0].User.ID = 8
			case "foreign marker":
				f.pulls[0].Body = "unowned"
			case "foreign commit":
				f.commit.Author.ID = 8
			case "different head":
				f.pulls[0].Head.SHA = strings.Repeat("b", 40)
			case "multiple open":
				other := f.pulls[0]
				other.Number = 4
				f.pulls = append(f.pulls, other)
			case "foreign head repository":
				f.pulls[0].Head.Repo.FullName = "other/fork"
			}
			if _, err := p.Invalidate(context.Background()); err == nil {
				t.Fatal("unsafe candidate accepted")
			}
			if len(mutations(f.calls)) != 0 {
				t.Fatalf("unsafe mutation: %v", f.calls)
			}
		})
	}
}

func TestInvalidateFailureAndLaterFreshness(t *testing.T) {
	for _, scenario := range []string{"disable forbidden", "close forbidden", "late stale", "default reassignment"} {
		t.Run(scenario, func(t *testing.T) {
			p, f := ownedFixture()
			switch scenario {
			case "disable forbidden":
				f.disableError = &githubapi.Error{Code: "github_http_failed", Status: 403}
			case "close forbidden":
				f.closeError = &githubapi.Error{Code: "github_http_failed", Status: 403}
			case "late stale":
				checks := 0
				p.Fresh = func(context.Context) error {
					checks++
					if checks > 1 {
						return errors.New("stale")
					}
					return nil
				}
			case "default reassignment":
				reads := 0
				f.onRepository = func() {
					reads++
					if reads > 1 {
						f.repository.DefaultBranch = p.Config.PromotionBranch
					}
				}
			}
			if _, err := p.Invalidate(context.Background()); err == nil {
				t.Fatal("failure accepted")
			}
			if scenario == "disable forbidden" && f.pulls[0].State != "open" {
				t.Fatal("close occurred after disarm failure")
			}
			if (scenario == "late stale" || scenario == "default reassignment") && len(mutations(f.calls)) != 0 {
				t.Fatalf("mutated after late guard failed: %v", f.calls)
			}
		})
	}
}
