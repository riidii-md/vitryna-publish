package githubpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/gitcmd"
	"github.com/riidii-md/vitryna-publish/internal/githubapi"
)

var ErrBaseMoved = errors.New("public_base_moved")

type TargetGitOptions struct {
	Remote, BaseBranch, PromotionBranch, BaseCommit string
	BotID                                           int64
	BotLogin                                        string
	ReadOnly                                        bool
}
type TargetGit struct {
	options   TargetGitOptions
	directory string
	owned     os.FileInfo
	git       *gitcmd.Publication
}
type Prepared struct {
	HasDiff      bool
	Commit, Tree string
}

func NewTargetGit(ctx context.Context, options TargetGitOptions, token string) (*TargetGit, error) {
	identityInvalid := options.BotID <= 0 || !regexp.MustCompile(`^[A-Za-z0-9-]+\[bot\]$`).MatchString(options.BotLogin)
	if options.ReadOnly {
		identityInvalid = token != "" || options.BotID != 0 || options.BotLogin != ""
	}
	if options.Remote == "" || options.BaseBranch == "" || options.PromotionBranch == options.BaseBranch || options.PromotionBranch == "" || !githubapi.ValidOID(options.BaseCommit) || identityInvalid {
		return nil, invalid("target_git_configuration_invalid")
	}
	if err := gitcmd.Capability(ctx); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp("", "vitryna-public-git-")
	if err != nil {
		return nil, invalid("target_git_stage_failed")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, invalid("target_git_stage_failed")
	}
	email := strconv.FormatInt(options.BotID, 10) + "+" + options.BotLogin + "@users.noreply.github.com"
	git := &TargetGit{options: options, directory: directory, owned: info, git: gitcmd.NewPublication(directory, options.Remote, token, options.BotLogin, email)}
	format := "sha1"
	if len(options.BaseCommit) == 64 {
		format = "sha256"
	}
	if _, err := git.git.Read(ctx, 4096, nil, "init", "--bare", "--template=", "--object-format="+format); err != nil {
		git.Close()
		return nil, err
	}
	for _, branch := range []string{options.BaseBranch, options.PromotionBranch} {
		if _, err := git.git.Read(ctx, 4096, nil, "check-ref-format", "--branch", branch); err != nil {
			git.Close()
			return nil, invalid("target_git_ref_invalid")
		}
	}
	return git, nil
}

func (g *TargetGit) Close() error {
	current, err := os.Lstat(g.directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !os.SameFile(g.owned, current) {
		return invalid("target_git_cleanup_ownership")
	}
	return os.RemoveAll(g.directory)
}

func (g *TargetGit) oid(ctx context.Context, input io.Reader, args ...string) (string, error) {
	data, err := g.git.Read(ctx, 512, input, args...)
	if err != nil {
		return "", err
	}
	oid := strings.TrimSpace(string(data))
	if !githubapi.ValidOID(oid) || len(oid) != len(g.options.BaseCommit) {
		return "", invalid("target_git_object_invalid")
	}
	return oid, nil
}

func (g *TargetGit) Prepare(ctx context.Context, directory string, receipt export.Receipt, promotionHead string) (Prepared, error) {
	var result Prepared
	if err := export.VerifyTree(ctx, directory, receipt); err != nil {
		return result, err
	}
	args := []string{"fetch", "--no-tags", "--no-write-fetch-head", "--depth=1", g.options.Remote, "refs/heads/" + g.options.BaseBranch + ":refs/vitryna/base"}
	if promotionHead != "" {
		if !githubapi.ValidOID(promotionHead) {
			return result, invalid("promotion_head_invalid")
		}
		args = append(args, "refs/heads/"+g.options.PromotionBranch+":refs/vitryna/promotion")
	}
	if _, err := g.git.Read(ctx, 4096, nil, args...); err != nil {
		return result, err
	}
	base, err := g.oid(ctx, nil, "rev-parse", "--verify", "refs/vitryna/base")
	if err != nil {
		return result, err
	}
	if base != g.options.BaseCommit {
		return result, ErrBaseMoved
	}
	if promotionHead != "" {
		head, err := g.oid(ctx, nil, "rev-parse", "--verify", "refs/vitryna/promotion")
		if err != nil {
			return result, err
		}
		if head != promotionHead {
			return result, invalid("promotion_head_changed")
		}
	}
	if _, err := g.git.Read(ctx, 4096, nil, "read-tree", "--empty"); err != nil {
		return result, err
	}
	var index strings.Builder
	for _, file := range receipt.Files {
		input, err := os.Open(filepath.Join(directory, filepath.FromSlash(file.Path)))
		if err != nil {
			return result, invalid("target_tree_read_failed")
		}
		hash := sha256.New()
		oid, hashErr := g.oid(ctx, io.TeeReader(io.LimitReader(input, file.Size+1), hash), "hash-object", "-w", "--stdin", "--no-filters")
		closeErr := input.Close()
		if hashErr != nil {
			return result, hashErr
		}
		if closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
			return result, invalid("target_tree_changed")
		}
		index.WriteString(file.Mode + " " + oid + "\t" + file.Path + "\x00")
	}
	if _, err := g.git.Read(ctx, 4096, strings.NewReader(index.String()), "update-index", "-z", "--index-info"); err != nil {
		return result, err
	}
	if err := export.VerifyTree(ctx, directory, receipt); err != nil {
		return result, err
	}
	result.Tree, err = g.oid(ctx, nil, "write-tree")
	if err != nil {
		return result, err
	}
	baseTree, err := g.oid(ctx, nil, "rev-parse", "--verify", "refs/vitryna/base^{tree}")
	if err != nil {
		return result, err
	}
	result.HasDiff = result.Tree != baseTree
	if result.HasDiff && !g.options.ReadOnly {
		result.Commit, err = g.oid(ctx, strings.NewReader(CommitMessage), "commit-tree", result.Tree, "-p", base)
	}
	return result, err
}

func (g *TargetGit) Push(ctx context.Context, commit, expectedHead, currentDefault string) error {
	if g.options.ReadOnly {
		return invalid("target_git_read_only")
	}
	if currentDefault == "" || g.options.PromotionBranch == currentDefault || g.options.PromotionBranch == g.options.BaseBranch {
		return invalid("promotion_ref_reserved")
	}
	if commit != "" && !githubapi.ValidOID(commit) || expectedHead != "" && !githubapi.ValidOID(expectedHead) {
		return invalid("promotion_head_invalid")
	}
	if commit == "" && expectedHead == "" {
		return invalid("promotion_delete_absent")
	}
	ref := "refs/heads/" + g.options.PromotionBranch
	spec := commit + ":" + ref
	_, err := g.git.Read(ctx, 1<<20, nil, "push", "--porcelain", "--no-verify", "--force-with-lease="+ref+":"+expectedHead, g.options.Remote, spec)
	return err
}
