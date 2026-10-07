package integration

import (
	"errors"
	"regexp"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
	"go.yaml.in/yaml/v3"
)

type hostedRecord struct {
	Source             string `json:"source"`
	Target             string `json:"target"`
	Version            string `json:"version"`
	SourceRepositoryID int64  `json:"sourceRepositoryId"`
	RunID              int64  `json:"runId"`
	ArtifactID         int64  `json:"artifactId"`
	BotID              int64  `json:"botId"`
	PullNumber         int    `json:"pullNumber"`
	SourceCommit       string `json:"sourceCommit"`
	PublicHead         string `json:"publicHead"`
	PublicParent       string `json:"publicParent"`
	Digest             string `json:"digest"`
	Base               string `json:"base"`
	Promotion          string `json:"promotion"`
	ExpectedAuto       bool   `json:"expectedAuto"`
}

func (r hostedRecord) validate() error {
	repository := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)
	if !repository.MatchString(r.Source) || !repository.MatchString(r.Target) || r.Source == r.Target || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9.-]*)?$`).MatchString(r.Version) || r.SourceRepositoryID <= 0 || r.RunID <= 0 || r.ArtifactID <= 0 || r.BotID <= 0 || r.PullNumber <= 0 || !githubapi.ValidOID(r.SourceCommit) || !githubapi.ValidOID(r.PublicHead) || !githubapi.ValidOID(r.PublicParent) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(r.Digest) {
		return errors.New("hosted_record_invalid")
	}
	data, err := yaml.Marshal(map[string]any{"version": 1, "export": map[string]any{"public": []string{"README.md"}, "private": []string{"private"}}, "publish": map[string]any{"github": config.GitHub{Repository: r.Target, BaseBranch: r.Base, PromotionBranch: r.Promotion, TrustedSourceBranch: "main", Merge: config.Merge{Mode: "manual"}}}})
	if err != nil {
		return errors.New("hosted_record_invalid")
	}
	_, err = config.Decode(data)
	return err
}
