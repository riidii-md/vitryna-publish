package export

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riidii-md/vitryna-publish/internal/gitcmd"
)

type ExportRequest struct {
	Policy       Policy
	ToolVersion  string
	ConfigSHA256 string
	OutputPath   string
	ReceiptPath  string
}

func (s *Snapshot) Export(ctx context.Context, req ExportRequest) (Receipt, error) {
	if req.ToolVersion == "" || !utf8.ValidString(req.ToolVersion) || int64(len(req.ToolVersion)) > MaxReceiptBytes || !validDigest(req.ConfigSHA256, 32) {
		return Receipt{}, &Diagnostic{Code: "export_metadata_invalid"}
	}
	compiled, err := req.Policy.compile()
	if err != nil {
		return Receipt{}, err
	}
	if _, err := s.check(ctx, compiled); err != nil {
		return Receipt{}, err
	}
	out, receiptPath, existing, err := s.destinations(req.OutputPath, req.ReceiptPath)
	if err != nil {
		return Receipt{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(out), ".vitryna-tree-")
	if err != nil {
		return Receipt{}, &Diagnostic{Code: "export_stage_failed"}
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		return Receipt{}, &Diagnostic{Code: "export_stage_failed"}
	}
	defer func() { _ = removeOwned(stage, stageInfo) }()
	r := Receipt{SchemaVersion: 1, ToolVersion: req.ToolVersion, SourceCommit: s.commit, ConfigSHA256: req.ConfigSHA256, PolicySHA256: req.Policy.Digest(), Limits: LimitsV1(), Files: []FileRecord{}}
	var compactBytes int64
	err = s.entries(ctx, func(entry treeEntry) error {
		visibility, _ := compiled.classify(entry.Path)
		if visibility != "public" {
			return nil
		}
		if len(r.Files) >= MaxFiles {
			return &Diagnostic{Code: "public_file_count_limit"}
		}
		size, err := s.blobSize(ctx, entry.OID)
		if err != nil {
			return err
		}
		if size > MaxBlobBytes {
			return &Diagnostic{Code: "public_blob_limit", Path: entry.Path}
		}
		if size > MaxTotalBytes-r.TotalBytes {
			return &Diagnostic{Code: "public_total_limit"}
		}
		path := filepath.Join(stage, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return &Diagnostic{Code: "export_write_failed", Path: entry.Path}
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return &Diagnostic{Code: "export_write_failed", Path: entry.Path}
		}
		hash := sha256.New()
		guard := contentGuard{destination: io.MultiWriter(file, hash), markers: req.Policy.ForbiddenContent, path: entry.Path}
		written, copyErr := gitcmd.Copy(ctx, s.repository, &guard, size, "cat-file", "blob", entry.OID)
		mode := os.FileMode(0644)
		if entry.Mode == "100755" {
			mode = 0755
		}
		modeErr := file.Chmod(mode)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if written != size || modeErr != nil || closeErr != nil {
			return &Diagnostic{Code: "export_write_failed", Path: entry.Path}
		}
		if size <= 1024 && lfsPointer(guard.prefix) {
			return &Diagnostic{Code: "public_lfs_unsupported", Path: entry.Path}
		}
		record := FileRecord{entry.Path, entry.Mode, size, hex.EncodeToString(hash.Sum(nil))}
		encoded, _ := json.Marshal(record)
		compactBytes += int64(len(encoded) + 1)
		if compactBytes > MaxReceiptBytes {
			return &Diagnostic{Code: "receipt_too_large"}
		}
		r.Files = append(r.Files, record)
		r.TotalBytes += size
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
	r.FileCount = len(r.Files)
	r.TreeSHA256 = treeDigest(r.Files)
	data, err := receiptBytes(r)
	if err != nil {
		return Receipt{}, err
	}
	if err := VerifyTree(ctx, stage, r); err != nil {
		return Receipt{}, err
	}
	if err := commitExport(ctx, stage, stageInfo, out, existing, receiptPath, data); err != nil {
		return Receipt{}, err
	}
	return r, nil
}

type contentGuard struct {
	destination   io.Writer
	markers       []string
	path          string
	carry, prefix []byte
}

func (w *contentGuard) Write(p []byte) (int, error) {
	if len(w.prefix) < 1024 {
		w.prefix = append(w.prefix, p[:min(len(p), 1024-len(w.prefix))]...)
	}
	if len(w.markers) > 0 {
		window := append(append([]byte{}, w.carry...), p...)
		maxLength := 0
		for _, marker := range w.markers {
			if bytes.Contains(window, []byte(marker)) {
				return 0, &Diagnostic{Code: "public_forbidden_content", Path: w.path}
			}
			maxLength = max(maxLength, len(marker))
		}
		keep := min(len(window), maxLength-1)
		w.carry = append(w.carry[:0], window[len(window)-keep:]...)
	}
	return w.destination.Write(p)
}

func lfsPointer(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || lines[0] != "version https://git-lfs.github.com/spec/v1" {
		return false
	}
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "oid sha256:") && validDigest(strings.TrimPrefix(line, "oid sha256:"), 32) {
			return true
		}
	}
	return false
}
