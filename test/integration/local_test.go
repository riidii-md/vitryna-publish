package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riidii-md/vitryna-publish/config"
	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/artifact"
	"github.com/riidii-md/vitryna-publish/internal/scaffold"
)

func command(t *testing.T, directory, executable string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(executable, args...)
	cmd.Dir = directory
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOROOT", "GOPATH", "GOTOOLCHAIN", "SystemRoot"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("synthetic %s command failed: %v\n%s", filepath.Base(executable), err, data)
	}
	return data
}

func write(t *testing.T, root, path string, body []byte, mode os.FileMode) {
	t.Helper()
	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuiltCLIOnboarding(t *testing.T) {
	project, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "vitryna")
	command(t, project, "go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-X main.version=v0.1.0-fixture", "-o", binary, "./cmd/vitryna")
	if got := string(command(t, project, binary, "version")); got != "vitryna v0.1.0-fixture\n" {
		t.Fatalf("wrong executable identity %q", got)
	}
	if !bytes.Contains(command(t, project, binary, "help"), []byte("init github")) {
		t.Fatal("installed fixture is not usable")
	}
	legal := sha256.Sum256(command(t, project, binary, "licenses"))
	if hex.EncodeToString(legal[:]) != "2507dc8ce9f010068ce67386117d05b77fe9cb8788b5d8d2a59235c81e46ce33" {
		t.Fatal("standalone executable omitted or changed complete reviewed notices")
	}
	source := t.TempDir()
	write(t, source, "README.md", []byte("Public fixture\n"), 0644)
	write(t, source, "private.txt", []byte("Synthetic private fixture\n"), 0644)
	command(t, source, "git", "init", "-q", "-b", "main")
	command(t, source, "git", "add", ".")
	command(t, source, "git", "commit", "-qm", "Synthetic source")
	pins := scaffold.ReleasePins{Version: "v0.1.0-fixture", ArtifactFormat: "github-zip-bundle-v1", Binaries: map[string]string{}}
	for _, platform := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"} {
		pins.Binaries[platform] = strings.Repeat("a", 64)
	}
	pinData, err := json.Marshal(pins)
	if err != nil {
		t.Fatal(err)
	}
	pinRoot := t.TempDir()
	write(t, pinRoot, "pins.json", pinData, 0600)
	initArgs := []string{"init", "github", "--repository", source, "--source-repository", "example/source", "--target", "example/public", "--public", "README.md", "--offline", "--pins", filepath.Join(pinRoot, "pins.json")}
	result := command(t, project, binary, initArgs...)
	if !bytes.Contains(result, []byte("intentionally fails")) || !bytes.Contains(read(t, filepath.Join(source, ".github/workflows/vitryna-publish.yml")), []byte("exit 1")) {
		t.Fatal("omitted verification command did not fail closed")
	}
	before := read(t, filepath.Join(source, ".vitryna.yml"))
	command(t, source, "git", "add", ".")
	command(t, source, "git", "commit", "-qm", "Reviewed fixture setup")
	command(t, project, binary, "check", "--repository", source, "--format", "json")
	if !bytes.Contains(command(t, project, binary, "explain", "--repository", source, "README.md"), []byte("public")) {
		t.Fatal("public visibility missing")
	}
	output := t.TempDir()
	tree, receipt := filepath.Join(output, "tree"), filepath.Join(output, "receipt.json")
	command(t, project, binary, "export", "--repository", source, "--output", tree, "--receipt", receipt)
	command(t, project, binary, "workflow", "verify-artifact", "--tree", tree, "--receipt", receipt)
	if !bytes.Equal(read(t, filepath.Join(tree, "README.md")), []byte("Public fixture\n")) {
		t.Fatal("onboarded tree bytes differ")
	}
	if _, err := os.Stat(filepath.Join(tree, "private.txt")); !os.IsNotExist(err) {
		t.Fatal("private file appeared in public edition")
	}
	if !bytes.Contains(command(t, project, binary, initArgs...), []byte("already matches")) || !bytes.Equal(before, read(t, filepath.Join(source, ".vitryna.yml"))) {
		t.Fatal("matching rerun was not non-mutating")
	}
}

func TestSyntheticTatamiNormalizedParity(t *testing.T) {
	fixture := "../../testdata/parity"
	var entries []struct {
		Path, Mode, Body string
	}
	if err := json.Unmarshal(read(t, filepath.Join(fixture, "source.json")), &entries); err != nil {
		t.Fatal(err)
	}
	var expected []export.FileRecord
	if err := json.Unmarshal(read(t, filepath.Join(fixture, "normalized-files.json")), &expected); err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		SchemaVersion                               int
		PublicPaths, PrivatePaths, ForbiddenContent []string
	}
	legacyData := read(t, filepath.Join(fixture, "legacy-policy.json"))
	if err := json.Unmarshal(legacyData, &legacy); err != nil || legacy.SchemaVersion != 1 {
		t.Fatal("invalid synthetic legacy policy")
	}
	configData := read(t, filepath.Join(fixture, "vitryna.yml"))
	cfg, err := config.Decode(configData)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ExportPolicy(), export.Policy{PublicPaths: legacy.PublicPaths, PrivatePaths: legacy.PrivatePaths, ForbiddenContent: legacy.ForbiddenContent}) {
		t.Fatal("mechanical conversion changed visibility")
	}
	source := t.TempDir()
	for _, entry := range entries {
		mode := os.FileMode(0644)
		if entry.Mode == "100755" {
			mode = 0755
		}
		write(t, source, entry.Path, []byte(entry.Body), mode)
	}
	write(t, source, ".public-export.json", legacyData, 0644)
	write(t, source, ".vitryna.yml", configData, 0644)
	command(t, source, "git", "init", "-q", "-b", "main")
	command(t, source, "git", "add", ".")
	command(t, source, "git", "commit", "-qm", "Synthetic parity")
	sha := strings.TrimSpace(string(command(t, source, "git", "rev-parse", "HEAD")))
	write(t, source, "README.md", []byte("Dirty content must not win\n"), 0644)
	write(t, source, ".vitryna.yml", []byte("invalid dirty config"), 0644)
	snapshot, err := export.Open(context.Background(), source, sha)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(configData)
	root := t.TempDir()
	tree, receiptPath := filepath.Join(root, "tree"), filepath.Join(root, "receipt.json")
	receipt, err := snapshot.Export(context.Background(), export.ExportRequest{Policy: cfg.ExportPolicy(), ToolVersion: "parity-fixture", ConfigSHA256: hex.EncodeToString(digest[:]), OutputPath: tree, ReceiptPath: receiptPath})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SourceCommit != sha || !reflect.DeepEqual(receipt.Files, expected) {
		t.Fatalf("normalized records differ: %#v", receipt.Files)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, "docs/private/") {
			if _, err := os.Stat(filepath.Join(tree, entry.Path)); !os.IsNotExist(err) {
				t.Fatal("private note exported")
			}
			continue
		}
		if !bytes.Equal(read(t, filepath.Join(tree, entry.Path)), []byte(entry.Body)) {
			t.Fatalf("byte parity failed for %s", entry.Path)
		}
	}
	if err := export.VerifyTree(context.Background(), tree, receipt); err != nil {
		t.Fatal(err)
	}
	if err := artifact.Seal(context.Background(), tree, receiptPath, filepath.Join(root, "seal")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("Mutation after sealing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := export.VerifyTree(context.Background(), tree, receipt); err == nil {
		t.Fatal("verification mutation was not detected")
	}
	if !bytes.Equal(read(t, filepath.Join(root, "seal/receipt.json")), read(t, receiptPath)) {
		t.Fatal("verification copy mutation changed original receipt")
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for _, path := range []string{"tree.tar", "receipt.json"} {
		writer, err := zw.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(read(t, filepath.Join(root, "seal", path))); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, root, "sealed.zip", archive.Bytes(), 0600)
	archiveDigest := sha256.Sum256(archive.Bytes())
	recovered, err := artifact.Extract(context.Background(), filepath.Join(root, "sealed.zip"), hex.EncodeToString(archiveDigest[:]), filepath.Join(root, "original"))
	if err != nil || !reflect.DeepEqual(recovered.Files, expected) {
		t.Fatal("original seal did not survive copy mutation")
	}
	if !bytes.Equal(read(t, filepath.Join(root, "original/tree/README.md")), []byte(entries[0].Body)) {
		t.Fatal("original seal bytes changed with verification copy")
	}
	if legacyBinary := os.Getenv("VITRYNA_LEGACY_EXPORT_BINARY"); legacyBinary != "" {
		legacyTree := filepath.Join(root, "legacy-tree")
		legacyReceipt := filepath.Join(root, "legacy-receipt.json")
		command(t, source, legacyBinary, "--repository", source, "--ref", sha, "--manifest", ".public-export.json", "--output", legacyTree, "--receipt", legacyReceipt)
		var old struct {
			SourceCommit string
			Files        []export.FileRecord
		}
		if err := json.Unmarshal(read(t, legacyReceipt), &old); err != nil || old.SourceCommit != sha || !reflect.DeepEqual(old.Files, expected) {
			t.Fatal("actual legacy exporter normalized parity failed")
		}
		if err := export.VerifyTree(context.Background(), legacyTree, recovered); err != nil {
			t.Fatal(err)
		}
	}
}
