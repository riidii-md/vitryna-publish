package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/riidii-md/vitryna-publish/internal/portablepath"
)

const (
	MaxBlobBytes     int64 = 64 << 20
	MaxTotalBytes    int64 = 512 << 20
	MaxFiles               = 100000
	MaxReceiptBytes  int64 = 16 << 20
	MaxArtifactBytes int64 = 1 << 30
)

type Limits struct {
	BlobBytes     int64 `json:"blobBytes"`
	TotalBytes    int64 `json:"totalBytes"`
	Files         int   `json:"files"`
	PathBytes     int   `json:"pathBytes"`
	ReceiptBytes  int64 `json:"receiptBytes"`
	ArtifactBytes int64 `json:"artifactBytes"`
}

func LimitsV1() Limits {
	return Limits{MaxBlobBytes, MaxTotalBytes, MaxFiles, portablepath.MaxBytes, MaxReceiptBytes, MaxArtifactBytes}
}

type FileRecord struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Receipt struct {
	SchemaVersion int          `json:"schemaVersion"`
	ToolVersion   string       `json:"toolVersion"`
	SourceCommit  string       `json:"sourceCommit"`
	ConfigSHA256  string       `json:"configSha256"`
	PolicySHA256  string       `json:"policySha256"`
	Limits        Limits       `json:"limits"`
	Files         []FileRecord `json:"files"`
	FileCount     int          `json:"fileCount"`
	TotalBytes    int64        `json:"totalBytes"`
	TreeSHA256    string       `json:"treeSha256"`
}

func treeDigest(files []FileRecord) string {
	data, _ := json.Marshal(files)
	hash := sha256.New()
	_, _ = hash.Write([]byte("vitryna-tree-v1\n"))
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func validDigest(value string, bytes int) bool {
	if len(value) != bytes*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func receiptBytes(receipt Receipt) ([]byte, error) {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || int64(len(data)+1) > MaxReceiptBytes {
		return nil, &Diagnostic{Code: "receipt_too_large"}
	}
	return append(data, '\n'), nil
}
