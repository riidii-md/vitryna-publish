package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/riidii-md/vitryna-publish/internal/portablepath"
)

func VerifyTree(ctx context.Context, directory string, receipt Receipt) error {
	if receipt.SchemaVersion != 1 || receipt.ToolVersion == "" || receipt.Limits != LimitsV1() || !validDigest(receipt.ConfigSHA256, 32) || !validDigest(receipt.PolicySHA256, 32) || !validDigest(receipt.TreeSHA256, 32) || !validDigest(receipt.SourceCommit, 20) && !validDigest(receipt.SourceCommit, 32) {
		return &Diagnostic{Code: "receipt_invalid"}
	}
	if receipt.FileCount != len(receipt.Files) || receipt.FileCount < 1 || receipt.FileCount > MaxFiles {
		return &Diagnostic{Code: "receipt_invalid"}
	}
	files := make(map[string]FileRecord)
	directories := map[string]bool{".": true}
	paths := portablepath.NewSet()
	var total, compactBytes int64
	previous := ""
	for _, file := range receipt.Files {
		if file.Path <= previous || file.Mode != "100644" && file.Mode != "100755" || file.Size < 0 || file.Size > MaxBlobBytes || file.Size > MaxTotalBytes-total || !validDigest(file.SHA256, 32) {
			return &Diagnostic{Code: "receipt_invalid"}
		}
		if err := paths.Add(file.Path); err != nil {
			return &Diagnostic{Code: "receipt_path_invalid", Path: file.Path}
		}
		total += file.Size
		compactBytes += int64(len(file.Path) + len(file.SHA256))
		if compactBytes > MaxReceiptBytes {
			return &Diagnostic{Code: "receipt_too_large"}
		}
		files[file.Path] = file
		for parent := filepath.ToSlash(filepath.Dir(file.Path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			directories[parent] = true
		}
		previous = file.Path
	}
	if receipt.TotalBytes != total || treeDigest(receipt.Files) != receipt.TreeSHA256 {
		return &Diagnostic{Code: "receipt_tree_digest"}
	}
	if _, err := receiptBytes(receipt); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(directory)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return &Diagnostic{Code: "verify_root_invalid"}
	}
	seen := make(map[string]bool)
	err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return &Diagnostic{Code: "verify_read_failed"}
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return &Diagnostic{Code: "verify_path_invalid"}
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if !directories[relative] {
				return &Diagnostic{Code: "verify_extra_path", Path: relative}
			}
			return nil
		}
		wanted, ok := files[relative]
		if !ok {
			return &Diagnostic{Code: "verify_extra_path", Path: relative}
		}
		info, err := entry.Info()
		mode := os.FileMode(0644)
		if wanted.Mode == "100755" {
			mode = 0755
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Mode().Perm() != mode || info.Size() != wanted.Size {
			return &Diagnostic{Code: "verify_file_metadata", Path: relative}
		}
		file, err := os.Open(path)
		if err != nil {
			return &Diagnostic{Code: "verify_read_failed", Path: relative}
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(file, wanted.Size+1))
		closeErr := file.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if readErr != nil || closeErr != nil || n != wanted.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), wanted.SHA256) {
			return &Diagnostic{Code: "verify_file_digest", Path: relative}
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(seen) != len(files) {
		return &Diagnostic{Code: "verify_missing_file"}
	}
	return nil
}
