package githubpublish

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

type nativeAutoAPI interface {
	API
	BranchRules(context.Context, string, string) ([]githubapi.BranchRule, error)
	ClassicProtection(context.Context, string, string) (json.RawMessage, error)
	EnableAutoMerge(context.Context, string, string, string) error
}

type reviewGate struct {
	Count        *int `json:"required_approving_review_count"`
	DismissStale bool `json:"dismiss_stale_reviews_on_push"`
	LastPush     bool `json:"require_last_push_approval"`
	CodeOwner    bool `json:"require_code_owner_review"`
	Reviewers    []struct {
		Count *int `json:"minimum_approvals"`
	} `json:"required_reviewers"`
	Methods *[]string `json:"allowed_merge_methods"`
}

func object(data json.RawMessage, target any) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' || json.Unmarshal(data, target) != nil {
		return invalid("automerge_protection_ambiguous")
	}
	return nil
}

func reviewFresh(count *int, codeOwner, dismiss, lastPush bool, conditional bool) (bool, error) {
	if count == nil || *count < 0 {
		return false, invalid("automerge_protection_ambiguous")
	}
	if (*count > 0 || codeOwner || conditional) && !dismiss && !lastPush {
		return false, invalid("automerge_review_freshness_required")
	}
	return *count > 0, nil
}

func rulesGate(rules []githubapi.BranchRule, method string) (bool, error) {
	gate := false
	for _, rule := range rules {
		if rule.Type == "" || rule.RulesetID <= 0 {
			return false, invalid("automerge_protection_ambiguous")
		}
		switch rule.Type {
		case "required_status_checks":
			var parameters struct {
				Checks *[]struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			}
			if err := object(rule.Parameters, &parameters); err != nil {
				return false, err
			}
			if parameters.Checks == nil {
				return false, invalid("automerge_protection_ambiguous")
			}
			for _, check := range *parameters.Checks {
				if strings.TrimSpace(check.Context) == "" {
					return false, invalid("automerge_protection_ambiguous")
				}
			}
			gate = gate || len(*parameters.Checks) > 0
		case "pull_request":
			var review reviewGate
			if err := object(rule.Parameters, &review); err != nil {
				return false, err
			}
			conditional := false
			for _, reviewer := range review.Reviewers {
				if reviewer.Count == nil || *reviewer.Count < 0 {
					return false, invalid("automerge_protection_ambiguous")
				}
				conditional = conditional || *reviewer.Count > 0
			}
			positive, err := reviewFresh(review.Count, review.CodeOwner, review.DismissStale, review.LastPush, conditional)
			if err != nil {
				return false, err
			}
			gate = gate || positive
			if review.Methods != nil {
				allowed := false
				for _, configured := range *review.Methods {
					if configured != "squash" && configured != "merge" && configured != "rebase" {
						return false, invalid("automerge_protection_ambiguous")
					}
					allowed = allowed || configured == method
				}
				if !allowed {
					return false, invalid("automerge_method_unavailable")
				}
			}
		case "merge_queue":
			var parameters struct {
				Method string `json:"merge_method"`
			}
			if err := object(rule.Parameters, &parameters); err != nil {
				return false, err
			}
			if parameters.Method != strings.ToUpper(method) {
				return false, invalid("automerge_queue_method_mismatch")
			}
			gate = true
		}
	}
	return gate, nil
}

func classicGate(data json.RawMessage) (bool, error) {
	var protection struct {
		Status *struct {
			Contexts []string `json:"contexts"`
			Checks   []struct {
				Context string `json:"context"`
			} `json:"checks"`
		} `json:"required_status_checks"`
		Review *struct {
			Count     *int `json:"required_approving_review_count"`
			Dismiss   bool `json:"dismiss_stale_reviews"`
			LastPush  bool `json:"require_last_push_approval"`
			CodeOwner bool `json:"require_code_owner_reviews"`
		} `json:"required_pull_request_reviews"`
	}
	if err := object(data, &protection); err != nil {
		return false, err
	}
	gate := false
	if protection.Status != nil {
		for _, name := range protection.Status.Contexts {
			if strings.TrimSpace(name) == "" {
				return false, invalid("automerge_protection_ambiguous")
			}
		}
		for _, check := range protection.Status.Checks {
			if strings.TrimSpace(check.Context) == "" {
				return false, invalid("automerge_protection_ambiguous")
			}
		}
		gate = len(protection.Status.Contexts) > 0 || len(protection.Status.Checks) > 0
	}
	if review := protection.Review; review != nil {
		positive, err := reviewFresh(review.Count, review.CodeOwner, review.Dismiss, review.LastPush, false)
		if err != nil {
			return false, err
		}
		gate = gate || positive
	}
	return gate, nil
}

func (p *Publisher) AutoPreflight(ctx context.Context) error {
	if p.Config.Merge.Mode == "" || p.Config.Merge.Mode == "manual" {
		return nil
	}
	api, ok := p.API.(nativeAutoAPI)
	if !ok || p.Config.Merge.Mode != "auto" {
		return invalid("automerge_configuration_invalid")
	}
	repository, err := p.target(ctx)
	if err != nil {
		return err
	}
	method := p.Config.Merge.Method
	allowed := method == "squash" && repository.AllowSquashMerge || method == "merge" && repository.AllowMergeCommit || method == "rebase" && repository.AllowRebaseMerge
	if !repository.AllowAutoMerge || !allowed {
		return invalid("automerge_method_unavailable")
	}
	if p.Config.Merge.ProtectionInspection != "effective_rulesets" && p.Config.Merge.ProtectionInspection != "classic" {
		return invalid("automerge_configuration_invalid")
	}
	rules, err := api.BranchRules(ctx, p.Config.Repository, p.Config.BaseBranch)
	if err != nil {
		return err
	}
	gate, err := rulesGate(rules, method)
	if err != nil {
		return err
	}
	if p.Config.Merge.ProtectionInspection == "classic" {
		protection, err := api.ClassicProtection(ctx, p.Config.Repository, p.Config.BaseBranch)
		if err != nil {
			return err
		}
		classic, err := classicGate(protection)
		if err != nil {
			return err
		}
		gate = gate || classic
	}
	if !gate {
		return invalid("automerge_blocking_gate_required")
	}
	return nil
}

func (p *Publisher) Preflight(ctx context.Context) (Result, error) {
	if p.Fresh == nil {
		return Result{}, invalid("publication_configuration_invalid")
	}
	if err := p.Fresh(ctx); err != nil {
		return Result{}, err
	}
	if _, err := p.target(ctx); err != nil {
		return Result{}, err
	}
	base, err := p.API.Ref(ctx, p.Config.Repository, p.Config.BaseBranch)
	if err != nil {
		return Result{}, err
	}
	if !githubapi.ValidOID(base) {
		return Result{}, invalid("public_base_missing")
	}
	if err := p.AutoPreflight(ctx); err != nil {
		return Result{}, err
	}
	return Result{Status: "preflight_ready"}, nil
}

func (p *Publisher) armedByThisApp(pull githubapi.PullRequest) bool {
	if pull.AutoMerge == nil {
		return false
	}
	var request struct {
		Method string         `json:"merge_method"`
		User   githubapi.User `json:"enabled_by"`
	}
	return object(*pull.AutoMerge, &request) == nil && request.Method == p.Config.Merge.Method && request.User.ID == p.BotID
}

func (p *Publisher) EnableNativeAutoMerge(ctx context.Context, expected githubapi.PullRequest) (result Result, resultErr error) {
	result = Result{Status: "pull_open", PullNumber: expected.Number}
	if p.Config.Merge.Mode == "" || p.Config.Merge.Mode == "manual" {
		return result, nil
	}
	defer func() {
		if resultErr != nil {
			result.Status = "pull_open_auto_denied"
		}
	}()
	api, ok := p.API.(nativeAutoAPI)
	if !ok {
		return result, invalid("automerge_configuration_invalid")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.AutoPreflight(ctx); err != nil {
			return result, err
		}
		pull, err := p.currentPull(ctx, expected)
		if err != nil {
			return result, err
		}
		if pull.Merged || pull.MergedAt != nil {
			return closedResult(pull), nil
		}
		if pull.State != "open" {
			return result, invalid("promotion_pull_state_changed")
		}
		if pull.AutoMerge != nil {
			if !p.armedByThisApp(pull) {
				return result, invalid("automerge_request_unowned")
			}
			result.Status = "auto_merge_enabled"
			result.Warning = "native_automerge_is_not_atomic_with_private_source_updates"
			return result, nil
		}
		if err := p.Fresh(ctx); err != nil {
			return result, err
		}
		err = api.EnableAutoMerge(ctx, pull.NodeID, p.Config.Merge.Method, pull.Head.SHA)
		observed, readErr := p.currentPull(ctx, expected)
		if readErr != nil {
			result.Warning = "native_automerge_state_unconfirmed_inspect_public_candidate"
			return result, readErr
		}
		if observed.Merged || observed.MergedAt != nil {
			return closedResult(observed), nil
		}
		if observed.State != "open" {
			return result, invalid("promotion_pull_state_changed")
		}
		if observed.AutoMerge != nil {
			if !p.armedByThisApp(observed) {
				return result, invalid("automerge_request_unowned")
			}
			result.Status = "auto_merge_enabled"
			result.Warning = "native_automerge_is_not_atomic_with_private_source_updates"
			return result, nil
		}
		if err == nil {
			return result, invalid("automerge_enable_unconfirmed")
		}
		if !ambiguous(err) {
			return result, err
		}
	}
	return result, invalid("automerge_enable_unconfirmed")
}
