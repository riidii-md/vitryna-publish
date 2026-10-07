package integration

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riidii-md/vitryna-publish/internal/artifact"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
	"github.com/riidii-md/vitryna-publish/internal/githubpublish"
)

func TestHostedRecordRejectsIncompleteOrAmbiguousInput(t *testing.T) {
	valid := hostedRecord{Source: "example/source", Target: "example/public", Version: "v0.1.0-rc.1", SourceRepositoryID: 1, RunID: 2, ArtifactID: 3, BotID: 4, PullNumber: 5, SourceCommit: strings.Repeat("a", 40), PublicHead: strings.Repeat("b", 40), PublicParent: strings.Repeat("c", 40), Digest: strings.Repeat("d", 64), Base: "main", Promotion: "vitryna/promote", ExpectedAuto: false}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*hostedRecord){
		func(r *hostedRecord) { r.Source = "../unsafe" },
		func(r *hostedRecord) { r.Target = r.Source },
		func(r *hostedRecord) { r.Version = "" },
		func(r *hostedRecord) { r.RunID = 0 },
		func(r *hostedRecord) { r.SourceRepositoryID = 0 },
		func(r *hostedRecord) { r.ArtifactID = 0 },
		func(r *hostedRecord) { r.BotID = 0 },
		func(r *hostedRecord) { r.PullNumber = 0 },
		func(r *hostedRecord) { r.SourceCommit = "bad" },
		func(r *hostedRecord) { r.PublicHead = "bad" },
		func(r *hostedRecord) { r.PublicParent = "bad" },
		func(r *hostedRecord) { r.Digest = strings.Repeat("D", 64) },
		func(r *hostedRecord) { r.Promotion = r.Base },
		func(r *hostedRecord) { r.Base = "bad branch" },
	} {
		r := valid
		mutate(&r)
		if r.validate() == nil {
			t.Fatal("unsafe hosted record accepted")
		}
	}
}

// This is a reader of an explicitly selected sandbox run, not a sandbox creator
// or publication driver. No App root key or target write token is accepted.
func TestHostedCandidateReadOnly(t *testing.T) {
	if os.Getenv("VITRYNA_HOSTED_ACCEPTANCE") != "read-only" {
		t.Skip("hosted acceptance pending; explicit read-only opt-in required")
	}
	recordPath := os.Getenv("VITRYNA_HOSTED_RECORD")
	info, err := os.Lstat(recordPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		t.Fatal("hosted record missing or invalid; no network request made")
	}
	var r hostedRecord
	data := read(t, recordPath)
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil || r.validate() != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("invalid hosted record; no network request made")
	}
	if os.Getenv("VITRYNA_SOURCE_TOKEN") == "" {
		t.Fatal("source read token required; never use an App private key here")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	source := githubapi.New(os.Getenv("VITRYNA_SOURCE_TOKEN"), nil)
	sourceRepo, err := source.Repository(ctx, r.Source)
	if err != nil || sourceRepo.ID != r.SourceRepositoryID || sourceRepo.FullName != r.Source || sourceRepo.Private == nil || !*sourceRepo.Private {
		t.Fatal("expected private sandbox source identity not established")
	}
	public := githubapi.New("", nil)
	target, err := public.Repository(ctx, r.Target)
	if err != nil || target.FullName != r.Target || target.ID <= 0 || target.Private == nil || *target.Private || target.DefaultBranch == r.Promotion {
		t.Fatal("expected public sandbox target identity not established")
	}
	root := t.TempDir()
	archive := filepath.Join(root, "original.zip")
	expected := githubapi.ArtifactExpectation{Repository: r.Source, RepositoryID: r.SourceRepositoryID, RunID: r.RunID, ArtifactID: r.ArtifactID, SourceCommit: r.SourceCommit, Digest: r.Digest}
	if err := source.DownloadArtifact(ctx, expected, archive); err != nil {
		t.Fatalf("original provider artifact rejected: %v", err)
	}
	bundle := filepath.Join(root, "bundle")
	receipt, err := artifact.Extract(ctx, archive, r.Digest, bundle)
	if err != nil || receipt.SourceCommit != r.SourceCommit || receipt.ToolVersion != r.Version {
		t.Fatal("original seal or receipt provenance rejected")
	}
	pull, err := public.PullRequest(ctx, r.Target, r.PullNumber)
	if err != nil || pull.State != "open" || pull.User.ID != r.BotID || pull.Head.Ref != r.Promotion || pull.Base.Ref != r.Base || pull.Head.Repo.ID != target.ID || pull.Base.Repo.ID != target.ID || pull.Head.SHA != r.PublicHead || !strings.Contains(pull.Body, githubpublish.BodyMarker) || (pull.AutoMerge != nil) != r.ExpectedAuto {
		t.Fatal("candidate identity, ownership, head, or expected auto state rejected")
	}
	commit, err := public.Commit(ctx, r.Target, r.PublicHead)
	if err != nil || commit.Author.ID != r.BotID || commit.Committer.ID != r.BotID || strings.TrimSpace(commit.Commit.Message) != strings.TrimSpace(githubpublish.CommitMessage) {
		t.Fatal("public commit actor/marker rejected")
	}
	var ancestry struct {
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := public.DoJSON(ctx, "GET", "/repos/"+r.Target+"/commits/"+r.PublicHead, nil, &ancestry); err != nil || len(ancestry.Parents) != 1 || ancestry.Parents[0].SHA != r.PublicParent {
		t.Fatal("one expected public parent not established")
	}
	git, err := githubpublish.NewTargetGit(ctx, githubpublish.TargetGitOptions{Remote: "https://github.com/" + r.Target + ".git", BaseBranch: r.Promotion, PromotionBranch: "vitryna/acceptance-read-" + strconv.Itoa(r.PullNumber), BaseCommit: r.PublicHead, ReadOnly: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer git.Close()
	comparison, err := git.Prepare(ctx, filepath.Join(bundle, "tree"), receipt, "")
	if err != nil || comparison.HasDiff {
		t.Fatal("public candidate tree differs from independently validated original seal")
	}
	t.Log("Read-only candidate evidence passed. App scope, secret isolation, checks/reviews, invalidation, and race scenarios still require the operator checklist; this alone is not P17 completion.")
}
