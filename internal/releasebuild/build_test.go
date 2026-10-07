package releasebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/internal/scaffold"
)

func TestReleaseContainsOnlyVersionedBinariesAndExactPins(t *testing.T) {
	output := filepath.Join(t.TempDir(), "release")
	build := func(_ context.Context, _, path, version, system, arch string) error {
		return os.WriteFile(path, []byte(version+" "+system+" "+arch), 0755)
	}
	result, err := buildRelease(context.Background(), ".", "v0.1.0-fixture", output, build)
	if err != nil || len(result.Names) != 6 {
		t.Fatalf("build %#v %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(output, "vitryna-pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	pins, err := scaffold.DecodePins(data, "v0.1.0-fixture")
	if err != nil {
		t.Fatal(err)
	}
	checksums, err := os.ReadFile(filepath.Join(output, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for platform, digest := range pins.Binaries {
		data, err := os.ReadFile(filepath.Join(output, "vitryna-"+platform))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if digest != hex.EncodeToString(hash[:]) || !strings.Contains(string(checksums), digest+"  vitryna-"+platform+"\n") {
			t.Fatal("checksum manifest not bound to actual binary bytes")
		}
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 6 {
		t.Fatal("unexpected release payload")
	}
	if _, err := buildRelease(context.Background(), ".", "v0.1.0-fixture", output, build); err == nil {
		t.Fatal("existing release overwritten")
	}
}

func TestReleaseFailureCleansOnlyOwnedStage(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "release")
	if err := os.WriteFile(filepath.Join(root, "existing"), []byte("preserve"), 0644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	build := func(_ context.Context, _, path, version, system, arch string) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		return os.WriteFile(path, []byte("partial"), 0755)
	}
	if _, err := buildRelease(context.Background(), ".", "v0.1.0-fixture", output, build); err == nil {
		t.Fatal("failed build accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "existing" {
		t.Fatal("failed release exposed partial output or removed user file")
	}
	if _, err := buildRelease(context.Background(), ".", "version;bad", output, build); err == nil {
		t.Fatal("unsafe version accepted")
	}
}
