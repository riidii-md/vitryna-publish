package artifact

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/ownedpath"
	"github.com/riidii-md/vitryna-publish/internal/portablepath"
)

func failure(code string) error { return &export.Diagnostic{Code: code} }

type cancellableReader struct {
	ctx   context.Context
	input io.Reader
}

func (r cancellableReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}

func decodeReceipt(data []byte) (export.Receipt, error) {
	var receipt export.Receipt
	if int64(len(data)) > export.MaxReceiptBytes {
		return receipt, failure("artifact_receipt_limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&receipt); err != nil {
		return receipt, failure("artifact_receipt_invalid")
	}
	canonical, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return receipt, failure("artifact_receipt_encoding")
	}
	return receipt, nil
}

func readBounded(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > max {
		return nil, failure("artifact_input_invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, failure("artifact_read_failed")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(data)) > max {
		return nil, failure("artifact_read_failed")
	}
	return data, nil
}

func VerifyBound(ctx context.Context, tree, receiptPath, expectedSHA256 string) (export.Receipt, error) {
	data, err := readBounded(receiptPath, export.MaxReceiptBytes)
	if err != nil {
		return export.Receipt{}, err
	}
	if expectedSHA256 != "" {
		decoded, err := hex.DecodeString(expectedSHA256)
		sum := sha256.Sum256(data)
		if err != nil || len(decoded) != 32 || expectedSHA256 != hex.EncodeToString(sum[:]) {
			return export.Receipt{}, failure("artifact_receipt_binding")
		}
	}
	receipt, err := decodeReceipt(data)
	if err != nil {
		return receipt, err
	}
	return receipt, export.VerifyTree(ctx, tree, receipt)
}

// Staging is always a new sibling; failure never cleans caller-owned paths.
func stage(destination string, excluded ...string) (ownedpath.Directory, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return ownedpath.Directory{}, failure("artifact_destination_invalid")
	}
	if _, err := os.Lstat(abs); !os.IsNotExist(err) {
		return ownedpath.Directory{}, failure("artifact_destination_exists")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return ownedpath.Directory{}, failure("artifact_destination_invalid")
	}
	for _, input := range excluded {
		root, err := filepath.EvalSymlinks(input)
		if err != nil {
			return ownedpath.Directory{}, failure("artifact_input_invalid")
		}
		info, err := os.Stat(root)
		if err != nil {
			return ownedpath.Directory{}, failure("artifact_input_invalid")
		}
		if !info.IsDir() {
			continue
		}
		for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
			current, err := os.Stat(ancestor)
			if err != nil {
				return ownedpath.Directory{}, failure("artifact_destination_invalid")
			}
			if os.SameFile(info, current) {
				return ownedpath.Directory{}, failure("artifact_destination_inside_input")
			}
			if ancestor == filepath.Dir(ancestor) {
				break
			}
		}
	}
	return ownedpath.TempDir(parent, ".vitryna-artifact-")
}

func expose(staging ownedpath.Directory, destination string) error {
	if err := staging.Check(); err != nil {
		return failure("artifact_stage_identity_changed")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return failure("artifact_destination_exists")
	}
	if err := os.Rename(staging.Path, destination); err != nil {
		return failure("artifact_commit_failed")
	}
	return nil
}

func Seal(ctx context.Context, tree, receiptPath, destination string) (resultErr error) {
	data, err := readBounded(receiptPath, export.MaxReceiptBytes)
	if err != nil {
		return err
	}
	receipt, err := decodeReceipt(data)
	if err != nil {
		return err
	}
	if err := export.VerifyTree(ctx, tree, receipt); err != nil {
		return err
	}
	staging, err := stage(destination, tree)
	if err != nil {
		return err
	}
	defer func() {
		if err := staging.Close(); err != nil {
			resultErr = failure("artifact_cleanup_failed")
		}
	}()
	file, err := os.OpenFile(filepath.Join(staging.Path, "tree.tar"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return failure("artifact_write_failed")
	}
	w := tar.NewWriter(file)
	for _, record := range receipt.Files {
		if ctx.Err() != nil {
			file.Close()
			return ctx.Err()
		}
		mode := int64(0644)
		if record.Mode == "100755" {
			mode = 0755
		}
		h := &tar.Header{Name: record.Path, Mode: mode, Size: record.Size, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}
		if err := w.WriteHeader(h); err != nil {
			file.Close()
			return failure("artifact_write_failed")
		}
		input, err := os.Open(filepath.Join(tree, filepath.FromSlash(record.Path)))
		if err != nil {
			file.Close()
			return failure("artifact_read_failed")
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(w, hash), cancellableReader{ctx, io.LimitReader(input, record.Size+1)})
		closeErr := input.Close()
		if ctx.Err() != nil {
			file.Close()
			return ctx.Err()
		}
		if copyErr != nil || closeErr != nil || n != record.Size || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
			file.Close()
			return failure("artifact_source_changed")
		}
	}
	if err := w.Close(); err != nil {
		file.Close()
		return failure("artifact_write_failed")
	}
	if err := file.Close(); err != nil {
		return failure("artifact_write_failed")
	}
	info, err := os.Stat(filepath.Join(staging.Path, "tree.tar"))
	if err != nil || info.Size()+int64(len(data)) > export.MaxArtifactBytes {
		return failure("artifact_size_limit")
	}
	if err := os.WriteFile(filepath.Join(staging.Path, "receipt.json"), data, 0600); err != nil {
		return failure("artifact_write_failed")
	}
	if err := export.VerifyTree(ctx, tree, receipt); err != nil {
		return err
	}
	return expose(staging, destination)
}

func Extract(ctx context.Context, archive, expectedSHA256, destination string) (export.Receipt, error) {
	return extract(ctx, archive, expectedSHA256, destination, "", "")
}

func ExtractBound(ctx context.Context, archive, expectedSHA256, destination, sourceCommit, toolVersion string) (export.Receipt, error) {
	if sourceCommit == "" || toolVersion == "" {
		return export.Receipt{}, failure("artifact_receipt_identity")
	}
	return extract(ctx, archive, expectedSHA256, destination, sourceCommit, toolVersion)
}

func extract(ctx context.Context, archive, expectedSHA256, destination, sourceCommit, toolVersion string) (receipt export.Receipt, resultErr error) {
	decoded, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(decoded) != 32 || expectedSHA256 != strings.ToLower(expectedSHA256) {
		return receipt, failure("artifact_digest_invalid")
	}
	info, err := os.Lstat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Size() > export.MaxArtifactBytes {
		return receipt, failure("artifact_size_limit")
	}
	f, err := os.Open(archive)
	if err != nil {
		return receipt, failure("artifact_read_failed")
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, cancellableReader{ctx, io.LimitReader(f, export.MaxArtifactBytes+1)})
	if ctx.Err() != nil {
		return receipt, ctx.Err()
	}
	if err != nil || n != info.Size() || hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		return receipt, failure("artifact_transport_digest")
	}
	z, err := zip.NewReader(f, n)
	if err != nil {
		return receipt, failure("artifact_zip_invalid")
	}
	if len(z.File) != 2 {
		return receipt, failure("artifact_zip_entries")
	}
	entries := make(map[string]*zip.File)
	var total uint64
	for _, entry := range z.File {
		if entry.Name != "tree.tar" && entry.Name != "receipt.json" || entries[entry.Name] != nil || !entry.Mode().IsRegular() || entry.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || entry.UncompressedSize64 > uint64(export.MaxArtifactBytes)-total {
			return receipt, failure("artifact_zip_entries")
		}
		total += entry.UncompressedSize64
		entries[entry.Name] = entry
	}
	if entries["receipt.json"].UncompressedSize64 > uint64(export.MaxReceiptBytes) {
		return receipt, failure("artifact_receipt_limit")
	}
	r, err := entries["receipt.json"].Open()
	if err != nil {
		return receipt, failure("artifact_zip_invalid")
	}
	data, err := io.ReadAll(io.LimitReader(r, export.MaxReceiptBytes+1))
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return receipt, failure("artifact_zip_invalid")
	}
	receipt, err = decodeReceipt(data)
	if err != nil {
		return receipt, err
	}
	if sourceCommit != "" && (receipt.SourceCommit != sourceCommit || receipt.ToolVersion != toolVersion) {
		return receipt, failure("artifact_receipt_identity")
	}
	// Bound and validate names before creating any extraction files. VerifyTree owns the complete receipt contract.
	if receipt.FileCount != len(receipt.Files) || receipt.FileCount < 1 || receipt.FileCount > export.MaxFiles {
		return receipt, failure("artifact_receipt_invalid")
	}
	paths := portablepath.NewSet()
	var declaredBytes int64
	for _, record := range receipt.Files {
		if err := paths.Add(record.Path); err != nil {
			return receipt, failure("artifact_path_invalid")
		}
		if record.Size < 0 || record.Size > export.MaxBlobBytes || record.Size > export.MaxTotalBytes-declaredBytes || record.Mode != "100644" && record.Mode != "100755" {
			return receipt, failure("artifact_receipt_invalid")
		}
		declaredBytes += record.Size
	}
	staging, err := stage(destination)
	if err != nil {
		return receipt, err
	}
	defer func() {
		if err := staging.Close(); err != nil {
			resultErr = failure("artifact_cleanup_failed")
		}
	}()
	tree := filepath.Join(staging.Path, "tree")
	if err := os.Mkdir(tree, 0700); err != nil {
		return receipt, failure("artifact_write_failed")
	}
	r, err = entries["tree.tar"].Open()
	if err != nil {
		return receipt, failure("artifact_zip_invalid")
	}
	err = extractTar(ctx, cancellableReader{ctx, io.LimitReader(r, export.MaxArtifactBytes+1)}, tree, receipt)
	closeErr = r.Close()
	if err != nil {
		return receipt, err
	}
	if closeErr != nil {
		return receipt, failure("artifact_zip_invalid")
	}
	if err := export.VerifyTree(ctx, tree, receipt); err != nil {
		return receipt, err
	}
	if err := os.WriteFile(filepath.Join(staging.Path, "receipt.json"), data, 0600); err != nil {
		return receipt, failure("artifact_write_failed")
	}
	if err := expose(staging, destination); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func extractTar(ctx context.Context, input io.Reader, tree string, receipt export.Receipt) error {
	r := tar.NewReader(input)
	for _, record := range receipt.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h, err := r.Next()
		if err != nil {
			return failure("artifact_tar_missing")
		}
		mode := int64(0644)
		if record.Mode == "100755" {
			mode = 0755
		}
		if h.Name != record.Path || h.Typeflag != tar.TypeReg || h.Size != record.Size || h.Mode != mode || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || h.Linkname != "" || !h.ModTime.Equal(time.Unix(0, 0)) || !h.AccessTime.IsZero() || !h.ChangeTime.IsZero() || h.Format != tar.FormatUSTAR && h.Format != tar.FormatPAX {
			return failure("artifact_tar_metadata")
		}
		for key, value := range h.PAXRecords {
			if key != "path" || value != record.Path {
				return failure("artifact_tar_extension")
			}
		}
		path := filepath.Join(tree, filepath.FromSlash(record.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return failure("artifact_write_failed")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(mode))
		if err != nil {
			return failure("artifact_write_failed")
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r, record.Size+1))
		chmodErr := f.Chmod(os.FileMode(mode))
		closeErr := f.Close()
		if copyErr != nil || chmodErr != nil || closeErr != nil || n != record.Size || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
			return failure("artifact_tar_digest")
		}
	}
	if _, err := r.Next(); err != io.EOF {
		return failure("artifact_tar_extra")
	}
	var trailing [1]byte
	if n, err := input.Read(trailing[:]); n != 0 || err != io.EOF {
		return failure("artifact_tar_trailing")
	}
	return ctx.Err()
}
