package export

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/riidii-md/vitryna-publish/internal/gitcmd"
	"github.com/riidii-md/vitryna-publish/internal/portablepath"
)

type Snapshot struct{ repository, commit string }
type treeEntry struct{ Path, Mode, Kind, OID string }

func Open(ctx context.Context, repository, ref string) (*Snapshot, error) {
	if err := gitcmd.Capability(ctx); err != nil {
		return nil, err
	}
	root, err := gitcmd.Read(ctx, repository, 8192, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(strings.TrimSuffix(string(root), "\n"))
	if err != nil {
		return nil, &Diagnostic{Code: "source_repository_invalid"}
	}
	commit, err := gitcmd.Read(ctx, resolved, 256, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(commit))
	if len(id) != 40 && len(id) != 64 {
		return nil, &Diagnostic{Code: "source_commit_invalid"}
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, &Diagnostic{Code: "source_commit_invalid"}
	}
	return &Snapshot{repository: resolved, commit: id}, nil
}

func (s *Snapshot) Commit() string { return s.commit }

func (s *Snapshot) entries(ctx context.Context, visit func(treeEntry) error) error {
	return gitcmd.Records(ctx, s.repository, func(record string) error {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || path == "" {
			return &Diagnostic{Code: "source_tree_invalid"}
		}
		return visit(treeEntry{Path: path, Mode: fields[0], Kind: fields[1], OID: fields[2]})
	}, "ls-tree", "-rz", "--full-tree", s.commit)
}

func (s *Snapshot) ReadFile(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	var selected *treeEntry
	if err := s.entries(ctx, func(entry treeEntry) error {
		if entry.Path == path {
			e := entry
			selected = &e
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, &Diagnostic{Code: "source_path_not_tracked", Path: path}
	}
	if selected.Kind != "blob" || selected.Mode != "100644" && selected.Mode != "100755" {
		return nil, &Diagnostic{Code: "source_file_mode", Path: path}
	}
	size, err := s.blobSize(ctx, selected.OID)
	if err != nil {
		return nil, err
	}
	if size > maxBytes || maxBytes < 0 {
		return nil, &Diagnostic{Code: "source_file_too_large", Path: path}
	}
	return gitcmd.Read(ctx, s.repository, maxBytes, "cat-file", "blob", selected.OID)
}

func (s *Snapshot) blobSize(ctx context.Context, oid string) (int64, error) {
	data, err := gitcmd.Read(ctx, s.repository, 128, "cat-file", "-s", oid)
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || size < 0 {
		return 0, &Diagnostic{Code: "source_object_size_invalid"}
	}
	return size, nil
}

type PathRecord struct {
	Path       string `json:"path"`
	Visibility string `json:"visibility"`
	Rule       string `json:"rule"`
	Mode       string `json:"mode"`
}
type CheckResult struct {
	SourceCommit string       `json:"sourceCommit"`
	Paths        []PathRecord `json:"paths"`
}
type Explanation struct {
	Path       string `json:"path"`
	Tracked    bool   `json:"tracked"`
	Visibility string `json:"visibility"`
	Rule       string `json:"rule"`
}

func (s *Snapshot) Check(ctx context.Context, p Policy) (CheckResult, error) {
	compiled, err := p.compile()
	if err != nil {
		return CheckResult{}, err
	}
	return s.check(ctx, compiled)
}

func (s *Snapshot) check(ctx context.Context, p compiledPolicy) (CheckResult, error) {
	result := CheckResult{SourceCommit: s.commit, Paths: []PathRecord{}}
	matched := make(map[string]bool)
	paths := portablepath.NewSet()
	if err := s.entries(ctx, func(e treeEntry) error {
		visibility, rule := p.classify(e.Path)
		if visibility == "" {
			return &Diagnostic{Code: "policy_unclassified", Path: e.Path}
		}
		if visibility == "public" {
			matched[rule] = true
			if e.Kind != "blob" || e.Mode != "100644" && e.Mode != "100755" {
				return &Diagnostic{Code: "public_mode_unsupported", Path: e.Path}
			}
			if err := paths.Add(e.Path); err != nil {
				return &Diagnostic{Code: "public_path_invalid", Path: e.Path}
			}
		}
		result.Paths = append(result.Paths, PathRecord{e.Path, visibility, rule, e.Mode})
		return nil
	}); err != nil {
		return CheckResult{}, err
	}
	for _, rule := range p.publicPaths {
		if !matched[rule] {
			return CheckResult{}, &Diagnostic{Code: "policy_public_rule_unmatched", Rule: rule}
		}
	}
	sort.Slice(result.Paths, func(i, j int) bool { return result.Paths[i].Path < result.Paths[j].Path })
	return result, nil
}

func (s *Snapshot) Explain(ctx context.Context, p Policy, path string) (Explanation, error) {
	compiled, err := p.compile()
	if err != nil {
		return Explanation{}, err
	}
	visibility, rule := compiled.classify(path)
	result := Explanation{Path: path, Visibility: visibility, Rule: rule}
	if err := s.entries(ctx, func(e treeEntry) error {
		if e.Path == path {
			result.Tracked = true
		}
		return nil
	}); err != nil {
		return Explanation{}, err
	}
	if result.Tracked && visibility == "" {
		return Explanation{}, &Diagnostic{Code: "policy_unclassified", Path: path}
	}
	return result, nil
}
