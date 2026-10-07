package githubapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

type BranchRule struct {
	Type       string          `json:"type"`
	RulesetID  int64           `json:"ruleset_id"`
	Parameters json.RawMessage `json:"parameters"`
}

func (c *Client) BranchRules(ctx context.Context, repository, branch string) ([]BranchRule, error) {
	var result []BranchRule
	for page := 1; page <= 5; page++ {
		var batch []BranchRule
		if err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/rules/branches/"+url.PathEscape(branch)+"?per_page=100&page="+strconv.Itoa(page), nil, &batch); err != nil {
			return nil, err
		}
		if batch == nil {
			return nil, &Error{Code: "github_rules_invalid"}
		}
		result = append(result, batch...)
		if len(batch) < 100 {
			return result, nil
		}
	}
	return nil, &Error{Code: "github_rules_limit"}
}

func (c *Client) ClassicProtection(ctx context.Context, repository, branch string) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.DoJSON(ctx, "GET", "/repos/"+repository+"/branches/"+url.PathEscape(branch)+"/protection", nil, &result)
	return result, err
}

func (c *Client) EnableAutoMerge(ctx context.Context, nodeID, method, expectedHead string) error {
	if nodeID == "" || !ValidOID(expectedHead) || method != "squash" && method != "merge" && method != "rebase" {
		return &Error{Code: "github_automerge_input_invalid"}
	}
	const query = `mutation($id:ID!,$method:PullRequestMergeMethod!,$head:GitObjectID!){enablePullRequestAutoMerge(input:{pullRequestId:$id,mergeMethod:$method,expectedHeadOid:$head}){pullRequest{id merged autoMergeRequest{mergeMethod}}}}`
	var result struct {
		Mutation struct {
			Pull struct {
				ID        string `json:"id"`
				Merged    bool   `json:"merged"`
				AutoMerge *struct {
					Method string `json:"mergeMethod"`
				} `json:"autoMergeRequest"`
			} `json:"pullRequest"`
		} `json:"enablePullRequestAutoMerge"`
	}
	if err := c.GraphQL(ctx, query, map[string]any{"id": nodeID, "method": strings.ToUpper(method), "head": expectedHead}, &result); err != nil {
		return err
	}
	pull := result.Mutation.Pull
	if pull.ID != nodeID || !pull.Merged && (pull.AutoMerge == nil || pull.AutoMerge.Method != strings.ToUpper(method)) {
		return &Error{Code: "github_automerge_enable_unconfirmed"}
	}
	return nil
}
