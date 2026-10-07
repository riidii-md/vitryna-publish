package githubpublish

import (
	"context"
	"errors"

	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

func ambiguous(err error) bool {
	var e *githubapi.Error
	return errors.As(err, &e) && (e.Code == "github_ambiguous_write" || e.Status >= 500)
}

func closedResult(pull githubapi.PullRequest) Result {
	result := Result{Status: "closed", PullNumber: pull.Number}
	if pull.Merged || pull.MergedAt != nil {
		result.Status = "already_merged"
		result.Warning = "candidate_merged_before_invalidation_non_atomic_race"
	}
	return result
}

func (p *Publisher) Invalidate(ctx context.Context) (result Result, resultErr error) {
	var knownCandidate int
	defer func() {
		if resultErr != nil && knownCandidate > 0 {
			result.PullNumber = knownCandidate
			result.Status = "invalidation_failed"
		}
	}()
	if p.Fresh == nil {
		return Result{}, invalid("publication_configuration_invalid")
	}
	if err := p.Fresh(ctx); err != nil {
		return Result{}, err
	}
	state, err := p.Inspect(ctx)
	if err != nil {
		return Result{}, err
	}
	if state.Open == nil {
		if state.Previous != nil {
			return closedResult(*state.Previous), nil
		}
		return Result{Status: "no_candidate"}, nil
	}
	expected := *state.Open
	knownCandidate = expected.Number
	pull, err := p.currentPull(ctx, expected)
	if err != nil {
		return Result{}, err
	}
	if pull.State == "closed" {
		return closedResult(pull), nil
	}
	if pull.AutoMerge != nil {
		for attempt := 0; attempt < 2; attempt++ {
			err = p.API.DisableAutoMerge(ctx, pull.NodeID)
			if err != nil && !ambiguous(err) {
				return Result{}, err
			}
			current, readErr := p.currentPull(ctx, expected)
			if readErr != nil {
				return Result{}, readErr
			}
			pull = current
			if pull.State == "closed" {
				return closedResult(pull), nil
			}
			if pull.AutoMerge == nil {
				break
			}
			if err == nil || attempt == 1 {
				return Result{}, invalid("automerge_disable_unconfirmed")
			}
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		pull, err = p.currentPull(ctx, expected)
		if err != nil {
			return Result{}, err
		}
		if pull.State == "closed" {
			return closedResult(pull), nil
		}
		if pull.AutoMerge != nil {
			return Result{}, invalid("automerge_rearmed")
		}
		err = p.API.ClosePullRequest(ctx, p.Config.Repository, pull.Number)
		if err != nil && !ambiguous(err) {
			return Result{}, err
		}
		current, readErr := p.currentPull(ctx, expected)
		if readErr != nil {
			return Result{}, readErr
		}
		if current.State == "closed" {
			return closedResult(current), nil
		}
		if err == nil || attempt == 1 {
			return Result{}, invalid("pull_close_unconfirmed")
		}
	}
	return Result{}, invalid("invalidation_unconfirmed")
}
