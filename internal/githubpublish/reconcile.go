package githubpublish

import (
	"context"
	"errors"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type ReconcileRequest struct {
	Directory               string
	Receipt                 export.Receipt
	Remote, BotLogin, Token string
}

type reconciliationAPI interface {
	API
	CreatePullRequest(context.Context, string, string, string) (githubapi.PullRequest, error)
	ReopenPullRequest(context.Context, string, int) (githubapi.PullRequest, error)
}

func (p *Publisher) reconcileState(ctx context.Context, expectedHead string) (State, error) {
	if err := p.Fresh(ctx); err != nil {
		return State{}, err
	}
	state, err := p.Inspect(ctx)
	if err != nil {
		return state, err
	}
	if state.Head != expectedHead {
		return state, invalid("promotion_head_changed")
	}
	return state, nil
}

func (p *Publisher) Reconcile(ctx context.Context, request ReconcileRequest) (result Result, resultErr error) {
	defer func() {
		if resultErr != nil && result.Status != "pull_open_auto_denied" {
			result.Status = "reconciliation_failed"
		}
	}()
	api, ok := p.API.(reconciliationAPI)
	if !ok || p.Fresh == nil {
		return result, invalid("publication_configuration_invalid")
	}
	if err := export.VerifyTree(ctx, request.Directory, request.Receipt); err != nil {
		return result, err
	}
	closed, err := p.Invalidate(ctx)
	result.PullNumber = closed.PullNumber
	if err != nil {
		return result, err
	}
	state, err := p.Inspect(ctx)
	if err != nil {
		return result, err
	}
	if state.Open != nil {
		return result, invalid("promotion_pull_reopened")
	}
	expectedHead := state.Head
	for attempt := 0; attempt < 2; attempt++ {
		base, err := p.API.Ref(ctx, p.Config.Repository, p.Config.BaseBranch)
		if err != nil {
			return result, err
		}
		if !githubapi.ValidOID(base) {
			return result, invalid("public_base_missing")
		}
		git, err := NewTargetGit(ctx, TargetGitOptions{Remote: request.Remote, BaseBranch: p.Config.BaseBranch, PromotionBranch: p.Config.PromotionBranch, BaseCommit: base, BotID: p.BotID, BotLogin: request.BotLogin}, request.Token)
		if err != nil {
			return result, err
		}
		prepared, prepareErr := git.Prepare(ctx, request.Directory, request.Receipt, expectedHead)
		if prepareErr != nil {
			cleanupErr := git.Close()
			if cleanupErr != nil {
				return result, cleanupErr
			}
			if errors.Is(prepareErr, ErrBaseMoved) {
				continue
			}
			return result, prepareErr
		}
		mutationErr := func() error {
			current, err := p.reconcileState(ctx, expectedHead)
			if err != nil {
				return err
			}
			if current.Open != nil {
				return invalid("promotion_pull_reopened")
			}
			currentBase, err := p.API.Ref(ctx, p.Config.Repository, p.Config.BaseBranch)
			if err != nil {
				return err
			}
			if currentBase != base {
				return ErrBaseMoved
			}
			if !prepared.HasDiff && expectedHead == "" {
				return nil
			}
			// Metadata and source freshness are checked as close to the leased write
			// as possible; administrator ref reassignment is not an atomic protocol.
			if err := p.Fresh(ctx); err != nil {
				return err
			}
			repository, err := p.target(ctx)
			if err != nil {
				return err
			}
			pushErr := git.Push(ctx, prepared.Commit, expectedHead, repository.DefaultBranch)
			observed, readErr := p.API.Ref(ctx, p.Config.Repository, p.Config.PromotionBranch)
			if readErr != nil {
				return readErr
			}
			if observed != prepared.Commit {
				if pushErr != nil {
					return pushErr
				}
				return invalid("promotion_push_unconfirmed")
			}
			// A lost push response is accepted only after observing the exact intended
			// head (or absence). A failed lease never authorizes an overriding retry.
			return nil
		}()
		cleanupErr := git.Close()
		if cleanupErr != nil {
			return result, cleanupErr
		}
		if errors.Is(mutationErr, ErrBaseMoved) {
			continue
		}
		if mutationErr != nil {
			return result, mutationErr
		}
		if !prepared.HasDiff {
			result.Status = "no_diff"
			return result, nil
		}
		pull, err := p.openPull(ctx, api, prepared.Commit)
		if pull.Number > 0 {
			result.PullNumber = pull.Number
		}
		if err != nil {
			return result, err
		}
		result.Status = "pull_open"
		if p.Config.Merge.Mode == "auto" {
			return p.EnableNativeAutoMerge(ctx, pull)
		}
		return result, nil
	}
	return result, ErrBaseMoved
}

func (p *Publisher) openPull(ctx context.Context, api reconciliationAPI, head string) (githubapi.PullRequest, error) {
	var empty githubapi.PullRequest
	var previous *githubapi.PullRequest
	create := false
	for attempt := 0; attempt < 3; attempt++ {
		state, err := p.reconcileState(ctx, head)
		if err != nil {
			return empty, err
		}
		if state.Open != nil {
			pull, err := p.currentPull(ctx, *state.Open)
			if err != nil {
				return pull, err
			}
			if pull.State != "open" || pull.AutoMerge != nil {
				return pull, invalid("promotion_pull_state_changed")
			}
			return pull, nil
		}
		if previous == nil {
			previous = state.Previous
		}
		if previous == nil || previous.Merged || previous.MergedAt != nil {
			create = true
		}
		var written githubapi.PullRequest
		if !create {
			current, readErr := p.API.PullRequest(ctx, p.Config.Repository, previous.Number)
			if readErr != nil {
				return empty, readErr
			}
			if !p.ownedPull(current, state.Repository) || current.NodeID != previous.NodeID || current.Head.SHA != head || current.State != "closed" || current.AutoMerge != nil || current.Merged || current.MergedAt != nil {
				return current, invalid("promotion_pull_state_changed")
			}
		}
		repository, err := p.target(ctx)
		if err != nil {
			return empty, err
		}
		if repository.ID != state.Repository.ID {
			return empty, invalid("target_metadata_changed")
		}
		if err := p.Fresh(ctx); err != nil {
			return empty, err
		}
		if create {
			written, err = api.CreatePullRequest(ctx, p.Config.Repository, p.Config.BaseBranch, p.Config.PromotionBranch)
		} else {
			written, err = api.ReopenPullRequest(ctx, p.Config.Repository, previous.Number)
		}
		if err == nil && (!p.ownedPull(written, state.Repository) || written.State != "open" || written.Head.SHA != head || written.AutoMerge != nil) {
			return written, invalid("promotion_pull_unowned")
		}
		observed, readErr := p.reconcileState(ctx, head)
		if readErr != nil {
			return empty, readErr
		}
		if observed.Open != nil {
			if err == nil && observed.Open.Number != written.Number {
				return empty, invalid("promotion_pull_ambiguous")
			}
			pull, readErr := p.currentPull(ctx, *observed.Open)
			if readErr != nil {
				return pull, readErr
			}
			if pull.State == "open" && pull.AutoMerge == nil {
				return pull, nil
			}
			return pull, invalid("promotion_pull_state_changed")
		}
		var providerErr *githubapi.Error
		if !create && errors.As(err, &providerErr) && providerErr.Status == 422 {
			current, readErr := p.API.PullRequest(ctx, p.Config.Repository, previous.Number)
			if readErr != nil {
				return empty, readErr
			}
			if !p.ownedPull(current, observed.Repository) || current.NodeID != previous.NodeID || current.Head.SHA != head || current.State != "closed" || current.AutoMerge != nil || current.Merged || current.MergedAt != nil {
				return current, invalid("promotion_pull_state_changed")
			}
			create = true
			continue
		}
		if err == nil {
			return written, invalid("pull_open_unconfirmed")
		}
		if !ambiguous(err) {
			return empty, err
		}
	}
	return empty, invalid("pull_open_unconfirmed")
}
