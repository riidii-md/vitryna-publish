package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/artifact"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
	"github.com/riidii-md/vitryna-publish/internal/githubpublish"
)

func compareCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("workflow compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tree := flags.String("tree", "", "receipt-checked original or unmodified verification copy")
	receiptPath := flags.String("receipt", "", "canonical receipt")
	receiptDigest := flags.String("receipt-sha256", "", "original receipt SHA-256 captured before project commands")
	target := flags.String("target", "", "public target owner/repo")
	base := flags.String("base", "", "public base branch")
	promotion := flags.String("promotion", "", "managed promotion branch")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *tree == "" || *receiptPath == "" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(*target) || *base == "" || *promotion == "" || *base == *promotion {
		fmt.Fprintln(stderr, "vitryna: compare requires receipt-bound tree and exact public target/base/promotion; use workflow compare --help")
		return 2
	}
	validBinding := true
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "receipt-sha256" {
			validBinding = regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*receiptDigest)
		}
	})
	if !validBinding {
		fmt.Fprintln(stderr, "vitryna: receipt-sha256 requires a lowercase SHA-256 digest")
		return 2
	}
	receipt, err := artifact.VerifyBound(ctx, *tree, *receiptPath, *receiptDigest)
	if err != nil {
		return failure(stderr, "json", err)
	}
	client := githubapi.New("", nil)
	repository, err := client.Repository(ctx, *target)
	if err != nil {
		return failure(stderr, "json", err)
	}
	if repository.ID <= 0 || repository.FullName != *target || repository.Private == nil || *repository.Private || repository.DefaultBranch == "" || repository.DefaultBranch == *promotion {
		return failure(stderr, "json", &export.Diagnostic{Code: "target_metadata_invalid"})
	}
	for attempt := 0; attempt < 2; attempt++ {
		baseCommit, err := client.Ref(ctx, *target, *base)
		if err != nil {
			return failure(stderr, "json", err)
		}
		if baseCommit == "" {
			return failure(stderr, "json", &export.Diagnostic{Code: "public_base_missing"})
		}
		git, err := githubpublish.NewTargetGit(ctx, githubpublish.TargetGitOptions{Remote: "https://github.com/" + *target + ".git", BaseBranch: *base, PromotionBranch: *promotion, BaseCommit: baseCommit, ReadOnly: true}, "")
		if err != nil {
			return failure(stderr, "json", err)
		}
		prepared, prepareErr := git.Prepare(ctx, *tree, receipt, "")
		if err := git.Close(); err != nil {
			return failure(stderr, "json", err)
		}
		if errors.Is(prepareErr, githubpublish.ErrBaseMoved) {
			continue
		}
		if prepareErr != nil {
			return failure(stderr, "json", prepareErr)
		}
		return jsonOutput(stdout, stderr, struct {
			HasDiff    bool   `json:"hasDiff"`
			PublicBase string `json:"publicBase"`
		}{prepared.HasDiff, baseCommit})
	}
	return failure(stderr, "json", githubpublish.ErrBaseMoved)
}
