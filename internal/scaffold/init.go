package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/gitcmd"
	"go.yaml.in/yaml/v3"
)

type Options struct {
	Repository, SourceRepository string
	GitHub                       config.GitHub
	PublicPaths                  []string
	VerifyCommand                string
	Pins                         ReleasePins
}
type Result struct {
	Files    []string `json:"files"`
	Existing bool     `json:"existing"`
}

func setupError(code string) error { return &export.Diagnostic{Code: code} }

func Init(ctx context.Context, options Options) (Result, error) {
	var result Result
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(options.SourceRepository) {
		return result, setupError("setup_source_identity_required")
	}
	for _, input := range []string{options.GitHub.Repository, options.GitHub.BaseBranch, options.GitHub.PromotionBranch, options.GitHub.TrustedSourceBranch, options.VerifyCommand} {
		if strings.Contains(input, "${{") {
			return result, setupError("setup_workflow_expression_forbidden")
		}
	}
	command, err := normalizeVerifyCommand(options.VerifyCommand)
	if err != nil {
		return result, err
	}
	options.VerifyCommand = command
	pinsJSON, err := json.Marshal(options.Pins)
	if err != nil {
		return result, pinError()
	}
	if _, err := DecodePins(pinsJSON, options.Pins.Version); err != nil {
		return result, err
	}
	snapshot, err := export.Open(ctx, options.Repository, "HEAD")
	if err != nil {
		return result, err
	}
	rootBytes, err := gitcmd.Read(ctx, options.Repository, 8192, "rev-parse", "--show-toplevel")
	if err != nil {
		return result, err
	}
	root, err := filepath.EvalSymlinks(strings.TrimSuffix(string(rootBytes), "\n"))
	if err != nil {
		return result, setupError("setup_repository_invalid")
	}
	private := map[string]bool{".vitryna.yml": true, ".github/workflows/vitryna-publish.yml": true, ".vitryna-setup.md": true}
	metadataBytes := 0
	for path := range private {
		metadataBytes += len(path) + 5
	}
	err = gitcmd.Records(ctx, root, func(path string) error {
		public := false
		for _, rule := range options.PublicPaths {
			if path == rule || strings.HasPrefix(path, rule+"/") {
				public = true
				break
			}
		}
		if !public && !private[path] {
			metadataBytes += len(path) + 5
			if metadataBytes > config.MaxBytes {
				return setupError("setup_inventory_too_large")
			}
			private[path] = true
		}
		return nil
	}, "ls-tree", "-rz", "--name-only", "--full-tree", snapshot.Commit())
	if err != nil {
		return result, err
	}
	privatePaths := make([]string, 0, len(private))
	for path := range private {
		privatePaths = append(privatePaths, path)
	}
	sort.Strings(privatePaths)
	publicPaths := append([]string(nil), options.PublicPaths...)
	sort.Strings(publicPaths)
	merge := map[string]string{"mode": options.GitHub.Merge.Mode}
	if merge["mode"] == "" {
		merge["mode"] = "manual"
	}
	if options.GitHub.Merge.Method != "" {
		merge["method"] = options.GitHub.Merge.Method
	}
	if options.GitHub.Merge.ProtectionInspection != "" {
		merge["protection_inspection"] = options.GitHub.Merge.ProtectionInspection
	}
	data, err := yaml.Marshal(map[string]any{"version": 1, "export": map[string]any{"public": publicPaths, "private": privatePaths}, "publish": map[string]any{"github": map[string]any{"repository": options.GitHub.Repository, "base_branch": options.GitHub.BaseBranch, "promotion_branch": options.GitHub.PromotionBranch, "trusted_source_branch": options.GitHub.TrustedSourceBranch, "merge": merge}}})
	if err != nil {
		return result, setupError("setup_config_encoding")
	}
	cfg, err := config.Decode(data)
	if err != nil {
		return result, err
	}
	if _, err := snapshot.Check(ctx, cfg.ExportPolicy()); err != nil {
		return result, err
	}
	options.GitHub = *cfg.GitHub
	workflow, err := RenderWorkflow(options)
	if err != nil {
		return result, err
	}
	guide, err := RenderGuide(options)
	if err != nil {
		return result, err
	}
	files := map[string][]byte{".vitryna.yml": data, ".github/workflows/vitryna-publish.yml": workflow, ".vitryna-setup.md": guide}
	for path := range files {
		result.Files = append(result.Files, path)
	}
	sort.Strings(result.Files)
	found := 0
	for _, path := range result.Files {
		if err := safeParents(root, path, false, nil); err != nil {
			return result, err
		}
		info, err := os.Lstat(filepath.Join(root, path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(files[path])) {
			return result, setupError("setup_existing_conflict")
		}
		existing, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !bytes.Equal(existing, files[path]) {
			return result, setupError("setup_existing_conflict")
		}
		found++
	}
	if found == len(files) {
		result.Existing = true
		return result, nil
	}
	if found != 0 {
		return result, setupError("setup_partial_conflict")
	}
	if err := writeFiles(ctx, root, files, os.Link); err != nil {
		return result, err
	}
	return result, nil
}

type ownedPath struct {
	path string
	info os.FileInfo
}

func safeParents(root, path string, create bool, created *[]ownedPath) error {
	parent := filepath.Dir(filepath.FromSlash(path))
	if parent == "." {
		return nil
	}
	current := root
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if !create {
				return nil
			}
			if err := os.Mkdir(current, 0755); err != nil {
				return setupError("setup_parent_create_failed")
			}
			info, err = os.Lstat(current)
			if err != nil {
				return setupError("setup_parent_create_failed")
			}
			*created = append(*created, ownedPath{current, info})
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return setupError("setup_parent_unsafe")
		}
	}
	return nil
}

func removeOwned(path ownedPath) {
	if current, err := os.Lstat(path.path); err == nil && os.SameFile(path.info, current) {
		_ = os.Remove(path.path)
	}
}

func writeFiles(ctx context.Context, root string, files map[string][]byte, linkFile func(string, string) error) (resultErr error) {
	var directories, staged, installed []ownedPath
	defer func() {
		for _, path := range staged {
			removeOwned(path)
		}
		if resultErr != nil {
			for _, path := range installed {
				removeOwned(path)
			}
			for i := len(directories) - 1; i >= 0; i-- {
				removeOwned(directories[i])
			}
		}
	}()
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := safeParents(root, path, true, &directories); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(filepath.Join(root, path)), ".vitryna-setup-*")
		if err != nil {
			return setupError("setup_stage_failed")
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return setupError("setup_stage_failed")
		}
		staged = append(staged, ownedPath{file.Name(), info})
		if _, err := file.Write(files[path]); err != nil {
			file.Close()
			return setupError("setup_write_failed")
		}
		if err := file.Chmod(0644); err != nil {
			file.Close()
			return setupError("setup_write_failed")
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return setupError("setup_write_failed")
		}
		if err := file.Close(); err != nil {
			return setupError("setup_write_failed")
		}
	}
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := safeParents(root, path, false, nil); err != nil {
			return err
		}
		target := filepath.Join(root, path)
		if err := linkFile(staged[i].path, target); err != nil {
			return setupError("setup_commit_failed")
		}
		installed = append(installed, ownedPath{target, staged[i].info})
	}
	return nil
}
