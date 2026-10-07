package githubpublish

import (
	"context"
	"reflect"
	"testing"

	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type mergedRaceAPI struct {
	*fakeAPI
	phase string
	reads int
}

func (f *mergedRaceAPI) merge() {
	f.pulls[0].State = "closed"
	f.pulls[0].Merged = true
	f.pulls[0].AutoMerge = nil
}

func (f *mergedRaceAPI) DisableAutoMerge(ctx context.Context, node string) error {
	if f.phase == "disarm" || f.phase == "ambiguous disarm" {
		f.calls = append(f.calls, "disable")
		f.merge()
		if f.phase == "ambiguous disarm" {
			return &githubapi.Error{Code: "github_ambiguous_write"}
		}
		return nil
	}
	return f.fakeAPI.DisableAutoMerge(ctx, node)
}

func (f *mergedRaceAPI) PullRequest(ctx context.Context, repository string, number int) (githubapi.PullRequest, error) {
	f.reads++
	// Read 1 precedes disarm, read 2 observes disarm, and read 3 is the
	// immediate pre-close read. Model provider merge at that last boundary.
	if f.phase == "before close" && f.reads == 3 {
		f.merge()
	}
	return f.fakeAPI.PullRequest(ctx, repository, number)
}

func TestMergeDuringInvalidation(t *testing.T) {
	for _, phase := range []string{"disarm", "ambiguous disarm", "before close"} {
		t.Run(phase, func(t *testing.T) {
			p, original := ownedFixture()
			f := &mergedRaceAPI{fakeAPI: original, phase: phase}
			p.API = f
			promotionBefore := f.head
			result, err := p.Invalidate(context.Background())
			if err != nil {
				t.Fatalf("provider merge must be reported as observed state, not failure: %v", err)
			}
			if result.Status != "already_merged" || result.PullNumber != 3 || result.Warning != "candidate_merged_before_invalidation_non_atomic_race" {
				t.Fatalf("missing promised P14 race recovery signal: %#v", result)
			}
			if !reflect.DeepEqual(mutations(f.calls), []string{"disable"}) {
				t.Fatalf("unexpected close after observing provider merge: %v", f.calls)
			}
			if f.head != promotionBefore || !f.pulls[0].Merged || f.pulls[0].State != "closed" {
				t.Fatalf("invalidation rewrote merged state or promotion identity: %s %#v", f.head, f.pulls[0])
			}
		})
	}
}
