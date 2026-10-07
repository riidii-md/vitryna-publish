package githubapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type Repository struct {
	ID               int64  `json:"id"`
	NodeID           string `json:"node_id"`
	FullName         string `json:"full_name"`
	Private          *bool  `json:"private"`
	DefaultBranch    string `json:"default_branch"`
	AllowAutoMerge   bool   `json:"allow_auto_merge"`
	AllowSquashMerge bool   `json:"allow_squash_merge"`
	AllowMergeCommit bool   `json:"allow_merge_commit"`
	AllowRebaseMerge bool   `json:"allow_rebase_merge"`
}

type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}
type PullRef struct {
	Ref  string     `json:"ref"`
	SHA  string     `json:"sha"`
	Repo Repository `json:"repo"`
}
type PullRequest struct {
	Number    int              `json:"number"`
	NodeID    string           `json:"node_id"`
	State     string           `json:"state"`
	User      User             `json:"user"`
	Body      string           `json:"body"`
	Head      PullRef          `json:"head"`
	Base      PullRef          `json:"base"`
	AutoMerge *json.RawMessage `json:"auto_merge"`
	Merged    bool             `json:"merged"`
	MergedAt  *string          `json:"merged_at"`
}

type Commit struct {
	SHA       string `json:"sha"`
	Author    User   `json:"author"`
	Committer User   `json:"committer"`
	Commit    struct {
		Message string `json:"message"`
	} `json:"commit"`
}

func ValidOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 || oid != strings.ToLower(oid) {
		return false
	}
	_, err := hex.DecodeString(oid)
	return err == nil
}

func (c *Client) Repository(ctx context.Context, repository string) (Repository, error) {
	var result Repository
	err := c.DoJSON(ctx, "GET", "/repos/"+repository, nil, &result)
	return result, err
}

func (c *Client) HasWorkflows(ctx context.Context, repository, commit string) (bool, error) {
	if !ValidOID(commit) {
		return false, &Error{Code: "github_commit_invalid"}
	}
	var entries []struct {
		Path string `json:"path"`
	}
	err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/contents/.github/workflows?ref="+commit, nil, &entries)
	var providerErr *Error
	if errors.As(err, &providerErr) && providerErr.Status == 404 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Path, ".github/workflows/") || strings.TrimPrefix(entry.Path, ".github/workflows/") == "" {
			return false, &Error{Code: "github_workflow_inventory_invalid"}
		}
	}
	return len(entries) != 0, nil
}

func (c *Client) Ref(ctx context.Context, repository, branch string) (string, error) {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/git/ref/heads/"+url.PathEscape(branch), nil, &ref)
	var e *Error
	if errors.As(err, &e) && e.Status == 404 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if ref.Ref != "refs/heads/"+branch || ref.Object.Type != "commit" || !ValidOID(ref.Object.SHA) {
		return "", &Error{Code: "github_ref_invalid"}
	}
	return ref.Object.SHA, nil
}

func (c *Client) Commit(ctx context.Context, repository, oid string) (Commit, error) {
	var commit Commit
	if !ValidOID(oid) {
		return commit, &Error{Code: "github_commit_invalid"}
	}
	err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/commits/"+oid, nil, &commit)
	if err != nil {
		return commit, err
	}
	if commit.SHA != oid {
		return commit, &Error{Code: "github_commit_invalid"}
	}
	return commit, nil
}

func (c *Client) WalkPullRequests(ctx context.Context, repository, base, head string, visit func(PullRequest) error) error {
	if visit == nil {
		return &Error{Code: "github_pull_visitor_invalid"}
	}
	owner, _, _ := strings.Cut(repository, "/")
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		query := url.Values{"state": {"all"}, "base": {base}, "head": {owner + ":" + head}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		var batch []PullRequest
		if err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/pulls?"+query.Encode(), nil, &batch); err != nil {
			return err
		}
		if len(batch) > 100 {
			return &Error{Code: "github_pull_page_invalid"}
		}
		for _, pull := range batch {
			if err := visit(pull); err != nil {
				return err
			}
		}
		if len(batch) < 100 {
			return nil
		}
	}
}

func (c *Client) PullRequest(ctx context.Context, repository string, number int) (PullRequest, error) {
	var result PullRequest
	if number <= 0 {
		return result, &Error{Code: "github_pull_invalid"}
	}
	err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/pulls/"+strconv.Itoa(number), nil, &result)
	if err == nil && result.Number != number {
		err = &Error{Code: "github_pull_invalid"}
	}
	return result, err
}

func (c *Client) ClosePullRequest(ctx context.Context, repository string, number int) error {
	if number <= 0 {
		return &Error{Code: "github_pull_invalid"}
	}
	var result PullRequest
	return c.DoJSON(ctx, "PATCH", "/repos/"+repository+"/pulls/"+strconv.Itoa(number), map[string]string{"state": "closed"}, &result)
}

func (c *Client) CreatePullRequest(ctx context.Context, repository, base, head string) (PullRequest, error) {
	var result PullRequest
	err := c.DoJSON(ctx, "POST", "/repos/"+repository+"/pulls", map[string]string{
		"base": base, "head": head, "title": "Update public repository",
		"body": "Public repository update.\n\n<!-- vitryna-publish:v1 -->",
	}, &result)
	if err == nil && (result.Number <= 0 || result.State != "open") {
		err = &Error{Code: "github_pull_invalid"}
	}
	return result, err
}

func (c *Client) ReopenPullRequest(ctx context.Context, repository string, number int) (PullRequest, error) {
	var result PullRequest
	if number <= 0 {
		return result, &Error{Code: "github_pull_invalid"}
	}
	err := c.DoJSON(ctx, "PATCH", "/repos/"+repository+"/pulls/"+strconv.Itoa(number), map[string]string{"state": "open"}, &result)
	if err == nil && (result.Number != number || result.State != "open") {
		err = &Error{Code: "github_pull_invalid"}
	}
	return result, err
}

func (c *Client) GraphQL(ctx context.Context, query string, variables map[string]any, output any) error {
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := c.DoJSON(ctx, "POST", "/graphql", map[string]any{"query": query, "variables": variables}, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) != 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return &Error{Code: "github_graphql_failed"}
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return &Error{Code: "github_graphql_response_invalid"}
	}
	return nil
}

func (c *Client) DisableAutoMerge(ctx context.Context, nodeID string) error {
	if nodeID == "" {
		return &Error{Code: "github_pull_invalid"}
	}
	const query = `mutation($id:ID!){disablePullRequestAutoMerge(input:{pullRequestId:$id}){pullRequest{id autoMergeRequest{enabledAt}}}}`
	var result struct {
		Mutation struct {
			Pull struct {
				ID        string           `json:"id"`
				AutoMerge *json.RawMessage `json:"autoMergeRequest"`
			} `json:"pullRequest"`
		} `json:"disablePullRequestAutoMerge"`
	}
	if err := c.GraphQL(ctx, query, map[string]any{"id": nodeID}, &result); err != nil {
		return err
	}
	if result.Mutation.Pull.ID != nodeID || result.Mutation.Pull.AutoMerge != nil {
		return &Error{Code: "github_automerge_disable_unconfirmed"}
	}
	return nil
}
