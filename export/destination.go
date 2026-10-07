package export

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return true
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	// Case aliases and alternate mount paths need inode identity, not text equality.
	for ancestor := path; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Stat(ancestor)
		if err == nil && os.SameFile(info, rootInfo) {
			return true
		}
		if filepath.Dir(ancestor) == ancestor {
			return false
		}
	}
}

func resolvedDestination(path string) (string, error) {
	if path == "" {
		return "", &Diagnostic{Code: "export_destination_missing"}
	}
	absolute, err := filepath.Abs(path)
	if err != nil || absolute == filepath.VolumeName(absolute)+string(os.PathSeparator) {
		return "", &Diagnostic{Code: "export_destination_invalid"}
	}
	ancestor := absolute
	var suffix []string
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if ancestor == absolute && info.Mode()&os.ModeSymlink != 0 {
				return "", &Diagnostic{Code: "export_destination_symlink"}
			}
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", &Diagnostic{Code: "export_destination_invalid"}
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", &Diagnostic{Code: "export_destination_invalid"}
		}
		suffix = append(suffix, filepath.Base(ancestor))
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", &Diagnostic{Code: "export_destination_invalid"}
		}
		ancestor = parent
	}
}

func (s *Snapshot) destinations(outputPath, receiptPath string) (string, string, os.FileInfo, error) {
	out, err := resolvedDestination(outputPath)
	if err != nil {
		return "", "", nil, err
	}
	receipt, err := resolvedDestination(receiptPath)
	if err != nil {
		return "", "", nil, err
	}
	if inside(s.repository, out) || inside(s.repository, receipt) || inside(out, receipt) {
		return "", "", nil, &Diagnostic{Code: "export_destination_containment"}
	}
	if _, err := os.Lstat(receipt); !os.IsNotExist(err) {
		return "", "", nil, &Diagnostic{Code: "export_receipt_exists"}
	}
	var existing os.FileInfo
	if info, err := os.Lstat(out); err == nil {
		if !info.IsDir() {
			return "", "", nil, &Diagnostic{Code: "export_output_exists"}
		}
		entries, err := os.ReadDir(out)
		if err != nil || len(entries) != 0 {
			return "", "", nil, &Diagnostic{Code: "export_output_nonempty"}
		}
		existing = info
	} else if !os.IsNotExist(err) {
		return "", "", nil, &Diagnostic{Code: "export_destination_invalid"}
	}
	for _, parent := range []string{filepath.Dir(out), filepath.Dir(receipt)} {
		if err := os.MkdirAll(parent, 0700); err != nil {
			return "", "", nil, &Diagnostic{Code: "export_destination_parent"}
		}
	}
	if inside(s.repository, out) || inside(s.repository, receipt) || inside(out, receipt) {
		return "", "", nil, &Diagnostic{Code: "export_destination_containment"}
	}
	return out, receipt, existing, nil
}

func removeOwned(path string, owner os.FileInfo) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || owner == nil || !os.SameFile(info, owner) {
		return &Diagnostic{Code: "export_owned_path_changed", Path: path}
	}
	return os.RemoveAll(path)
}

func commitExport(ctx context.Context, stage string, stageInfo os.FileInfo, out string, original os.FileInfo, receiptPath string, data []byte) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	var backup string
	var exposed bool
	var receiptOwner os.FileInfo
	defer func() {
		if result == nil {
			return
		}
		if receiptOwner != nil {
			if err := removeOwned(receiptPath, receiptOwner); err != nil {
				result = err
			}
		}
		if exposed {
			if err := removeOwned(out, stageInfo); err != nil {
				result = err
			}
		}
		if backup != "" {
			if _, err := os.Lstat(out); !os.IsNotExist(err) {
				result = &Diagnostic{Code: "export_rollback_retained", Path: backup}
				return
			}
			if err := os.Rename(backup, out); err != nil {
				result = &Diagnostic{Code: "export_rollback_retained", Path: backup}
			}
		}
	}()
	if original != nil {
		current, err := os.Lstat(out)
		if err != nil || !os.SameFile(current, original) {
			return &Diagnostic{Code: "export_output_changed"}
		}
		entries, err := os.ReadDir(out)
		if err != nil || len(entries) != 0 {
			return &Diagnostic{Code: "export_output_changed"}
		}
		backup, err = os.MkdirTemp(filepath.Dir(out), ".vitryna-original-")
		if err != nil {
			return &Diagnostic{Code: "export_commit_failed"}
		}
		if err := os.Rename(out, backup); err != nil {
			_ = os.Remove(backup)
			backup = ""
			return &Diagnostic{Code: "export_commit_failed"}
		}
		if err := os.Chmod(stage, original.Mode().Perm()); err != nil {
			return &Diagnostic{Code: "export_commit_failed"}
		}
	} else if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return &Diagnostic{Code: "export_output_changed"}
	}
	if err := os.Rename(stage, out); err != nil {
		return &Diagnostic{Code: "export_commit_failed"}
	}
	exposed = true
	if err := ctx.Err(); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(receiptPath), ".vitryna-receipt-")
	if err != nil {
		return &Diagnostic{Code: "receipt_write_failed"}
	}
	defer os.Remove(temporary.Name())
	_, writeErr := temporary.Write(data)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return &Diagnostic{Code: "receipt_write_failed"}
	}
	if err := os.Link(temporary.Name(), receiptPath); err != nil {
		return &Diagnostic{Code: "receipt_write_failed"}
	}
	receiptOwner, err = os.Lstat(receiptPath)
	if err != nil {
		return &Diagnostic{Code: "receipt_write_failed"}
	}
	if backup != "" {
		if err := os.Remove(backup); err != nil {
			return &Diagnostic{Code: "export_original_changed", Path: backup}
		}
		backup = ""
	}
	return nil
}
