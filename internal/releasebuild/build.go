package releasebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/riidii-md/vitryna-publish/export"
	"github.com/riidii-md/vitryna-publish/internal/scaffold"
)

type Result struct {
	Directory string   `json:"directory"`
	Names     []string `json:"names"`
}
type builder func(context.Context, string, string, string, string, string) error

func failure(code string) error { return &export.Diagnostic{Code: code} }

func Build(ctx context.Context, repository, version, output string) (Result, error) {
	root, err := filepath.Abs(repository)
	if err != nil {
		return Result{}, failure("release_repository_invalid")
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || !info.Mode().IsRegular() {
		return Result{}, failure("release_repository_invalid")
	}
	return buildRelease(ctx, root, version, output, buildGo)
}

func buildGo(ctx context.Context, repository, path, version, system, arch string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X main.version="+version, "-o", path, "./cmd/vitryna")
	cmd.Dir = repository
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		switch name {
		case "PATH", "HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "GOPROXY", "GOSUMDB", "GOTOOLCHAIN", "TMPDIR", "TMP", "TEMP", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR":
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0", "GOOS="+system, "GOARCH="+arch, "GOWORK=off", "GOENV=off", "GOFLAGS=")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure("release_build_failed")
	}
	return nil
}

func ownedRemove(directory string, owned os.FileInfo) {
	if current, err := os.Lstat(directory); err == nil && os.SameFile(owned, current) {
		_ = os.RemoveAll(directory)
	}
}

func buildRelease(ctx context.Context, repository, version, output string, build builder) (result Result, resultErr error) {
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`).MatchString(version) || output == "" {
		return result, failure("release_arguments_invalid")
	}
	output, err := filepath.Abs(output)
	if err != nil {
		return result, failure("release_destination_invalid")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return result, failure("release_destination_exists")
	}
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return result, failure("release_destination_invalid")
	}
	stage, err := os.MkdirTemp(parent, ".vitryna-release-*")
	if err != nil {
		return result, failure("release_stage_failed")
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		return result, failure("release_stage_failed")
	}
	defer ownedRemove(stage, stageInfo)
	pins := scaffold.ReleasePins{Version: version, ArtifactFormat: "github-zip-bundle-v1", Binaries: map[string]string{}}
	for _, platform := range []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"} {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		system, arch, _ := strings.Cut(platform, "-")
		name := "vitryna-" + platform
		path := filepath.Join(stage, name)
		if err := build(ctx, repository, path, version, system, arch); err != nil {
			return result, err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return result, failure("release_binary_invalid")
		}
		file, err := os.Open(path)
		if err != nil {
			return result, failure("release_binary_invalid")
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return result, failure("release_binary_invalid")
		}
		pins.Binaries[platform] = hex.EncodeToString(hash.Sum(nil))
		result.Names = append(result.Names, name)
	}
	encoded, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return result, failure("release_pins_invalid")
	}
	encoded = append(encoded, '\n')
	if _, err := scaffold.DecodePins(encoded, version); err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(stage, "vitryna-pins.json"), encoded, 0644); err != nil {
		return result, failure("release_write_failed")
	}
	var checksums strings.Builder
	for _, name := range result.Names {
		checksums.WriteString(pins.Binaries[strings.TrimPrefix(name, "vitryna-")] + "  " + name + "\n")
	}
	manifestHash := sha256.Sum256(encoded)
	checksums.WriteString(hex.EncodeToString(manifestHash[:]) + "  vitryna-pins.json\n")
	if err := os.WriteFile(filepath.Join(stage, "checksums.txt"), []byte(checksums.String()), 0644); err != nil {
		return result, failure("release_write_failed")
	}
	result.Names = append(result.Names, "vitryna-pins.json", "checksums.txt")
	sort.Strings(result.Names)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return result, failure("release_destination_exists")
	}
	owned, err := os.Lstat(output)
	if err != nil {
		return result, failure("release_destination_invalid")
	}
	defer func() {
		if resultErr != nil {
			ownedRemove(output, owned)
		}
	}()
	for _, name := range result.Names {
		if err := os.Link(filepath.Join(stage, name), filepath.Join(output, name)); err != nil {
			return result, failure("release_commit_failed")
		}
	}
	result.Directory = output
	return result, nil
}
