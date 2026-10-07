package githubpublish

import (
	"context"
	"strings"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

const BodyMarker = "<!-- vitryna-publish:v1 -->"
const CommitMessage = "Update public repository\n\nVitryna-Publish: v1\n"

type API interface {
	Repository(context.Context, string) (githubapi.Repository, error)
	Ref(context.Context, string, string) (string, error)
	Commit(context.Context, string, string) (githubapi.Commit, error)
	WalkPullRequests(context.Context, string, string, string, func(githubapi.PullRequest) error) error
	PullRequest(context.Context, string, int) (githubapi.PullRequest, error)
	DisableAutoMerge(context.Context, string) error
	ClosePullRequest(context.Context, string, int) error
}

type Publisher struct {
	Config config.GitHub
	API    API
	BotID  int64
	Fresh  func(context.Context) error
}

type State struct {
	Repository     githubapi.Repository
	Head           string
	Open, Previous *githubapi.PullRequest
}
type Result struct {
	Status     string `json:"status"`
	PullNumber int    `json:"pullNumber,omitempty"`
	Warning    string `json:"warning,omitempty"`
}

func invalid(code string) error { return &export.Diagnostic{Code: code} }

func (p *Publisher) target(ctx context.Context) (githubapi.Repository, error) {
	var empty githubapi.Repository
	if p.API == nil || p.Fresh == nil || p.BotID <= 0 || p.Config.Repository == "" || p.Config.BaseBranch == "" || p.Config.PromotionBranch == "" {
		return empty, invalid("publication_configuration_invalid")
	}
	if p.Config.BaseBranch == p.Config.PromotionBranch {
		return empty, invalid("promotion_ref_reserved")
	}
	repository, err := p.API.Repository(ctx, p.Config.Repository)
	if err != nil {
		return empty, err
	}
	if repository.ID <= 0 || repository.FullName != p.Config.Repository || repository.Private == nil || *repository.Private || repository.DefaultBranch == "" {
		return empty, invalid("target_metadata_invalid")
	}
	if repository.DefaultBranch == p.Config.PromotionBranch {
		return empty, invalid("promotion_ref_reserved")
	}
	return repository, nil
}

func (p *Publisher) ownedCommit(commit githubapi.Commit, oid string) bool {
	return githubapi.ValidOID(oid) && commit.SHA == oid && commit.Author.ID == p.BotID && commit.Committer.ID == p.BotID && strings.TrimSpace(commit.Commit.Message) == strings.TrimSpace(CommitMessage)
}

func (p *Publisher) ownedPull(pull githubapi.PullRequest, repository githubapi.Repository) bool {
	marker := false
	for _, line := range strings.Split(pull.Body, "\n") {
		if line == BodyMarker {
			marker = true
		}
	}
	return pull.Number > 0 && pull.NodeID != "" && (pull.State == "open" || pull.State == "closed") && pull.User.ID == p.BotID && marker && pull.Base.Ref == p.Config.BaseBranch && pull.Head.Ref == p.Config.PromotionBranch && githubapi.ValidOID(pull.Head.SHA) && pull.Base.Repo.ID == repository.ID && pull.Head.Repo.ID == repository.ID && pull.Base.Repo.FullName == p.Config.Repository && pull.Head.Repo.FullName == p.Config.Repository
}

func (p *Publisher) Inspect(ctx context.Context) (State, error) {
	var state State
	repository, err := p.target(ctx)
	if err != nil {
		return state, err
	}
	state.Repository = repository
	state.Head, err = p.API.Ref(ctx, p.Config.Repository, p.Config.PromotionBranch)
	if err != nil {
		return state, err
	}
	if state.Head != "" {
		commit, err := p.API.Commit(ctx, p.Config.Repository, state.Head)
		if err != nil {
			return state, err
		}
		if !p.ownedCommit(commit, state.Head) {
			return state, invalid("promotion_ref_unowned")
		}
	}
	seen := make(map[int]bool)
	err = p.API.WalkPullRequests(ctx, p.Config.Repository, p.Config.BaseBranch, p.Config.PromotionBranch, func(pull githubapi.PullRequest) error {
		if seen[pull.Number] || !p.ownedPull(pull, repository) {
			return invalid("promotion_pull_unowned")
		}
		seen[pull.Number] = true
		if pull.State == "open" {
			if state.Open != nil {
				return invalid("promotion_pull_ambiguous")
			}
			if state.Head != "" && pull.Head.SHA != state.Head {
				return invalid("promotion_head_changed")
			}
			if state.Head == "" {
				commit, err := p.API.Commit(ctx, p.Config.Repository, pull.Head.SHA)
				if err != nil {
					return err
				}
				if !p.ownedCommit(commit, pull.Head.SHA) {
					return invalid("promotion_ref_unowned")
				}
			}
			copy := pull
			state.Open = &copy
		} else if state.Previous == nil || pull.Number > state.Previous.Number {
			copy := pull
			state.Previous = &copy
		}
		return nil
	})
	return state, err
}

func (p *Publisher) currentPull(ctx context.Context, expected githubapi.PullRequest) (githubapi.PullRequest, error) {
	if err := p.Fresh(ctx); err != nil {
		return githubapi.PullRequest{}, err
	}
	state, err := p.Inspect(ctx)
	if err != nil {
		return githubapi.PullRequest{}, err
	}
	pull, err := p.API.PullRequest(ctx, p.Config.Repository, expected.Number)
	if err != nil {
		return pull, err
	}
	if !p.ownedPull(pull, state.Repository) || pull.Head.SHA != expected.Head.SHA || pull.NodeID != expected.NodeID || state.Open != nil && state.Open.Number != expected.Number {
		return pull, invalid("promotion_head_changed")
	}
	return pull, nil
}
