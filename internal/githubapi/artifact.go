package githubapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/riidii-md/vitryna-publish/export"
)

type ArtifactExpectation struct {
	Repository                      string
	RepositoryID, RunID, ArtifactID int64
	HeadRepositoryID                int64
	SourceCommit, Digest            string
}

func digestValue(value string) (string, bool) {
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 || value != strings.ToLower(value) {
		return "", false
	}
	_, err := hex.DecodeString(value)
	return value, err == nil
}

func (c *Client) DownloadArtifact(ctx context.Context, expected ArtifactExpectation, destination string) error {
	if expected.HeadRepositoryID == 0 {
		expected.HeadRepositoryID = expected.RepositoryID
	}
	digest, ok := digestValue(expected.Digest)
	if !ok || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`).MatchString(expected.Repository) || expected.RepositoryID <= 0 || expected.HeadRepositoryID <= 0 || expected.RunID <= 0 || expected.ArtifactID <= 0 || !ValidOID(expected.SourceCommit) {
		return &Error{Code: "github_artifact_expectation_invalid"}
	}
	endpoint := "/repos/" + expected.Repository + "/actions/artifacts/" + strconv.FormatInt(expected.ArtifactID, 10)
	var metadata struct {
		ID        int64     `json:"id"`
		Size      int64     `json:"size_in_bytes"`
		Expired   *bool     `json:"expired"`
		ExpiresAt time.Time `json:"expires_at"`
		Digest    string    `json:"digest"`
		Run       struct {
			ID               int64  `json:"id"`
			RepositoryID     int64  `json:"repository_id"`
			HeadRepositoryID int64  `json:"head_repository_id"`
			HeadSHA          string `json:"head_sha"`
		} `json:"workflow_run"`
	}
	if err := c.DoJSON(ctx, "GET", endpoint, nil, &metadata); err != nil {
		return err
	}
	actual, valid := digestValue(metadata.Digest)
	if !valid || actual != digest || metadata.ID != expected.ArtifactID || metadata.Size <= 0 || metadata.Size > export.MaxArtifactBytes || metadata.Expired == nil || *metadata.Expired || !metadata.ExpiresAt.After(time.Now()) || metadata.Run.ID != expected.RunID || metadata.Run.RepositoryID != expected.RepositoryID || metadata.Run.HeadRepositoryID != expected.HeadRepositoryID || metadata.Run.HeadSHA != expected.SourceCommit {
		return &Error{Code: "github_artifact_provenance"}
	}
	response, err := c.request(ctx, "GET", endpoint+"/zip", nil)
	if err != nil {
		return err
	}
	for redirects := 0; response.StatusCode >= 300 && response.StatusCode < 400; redirects++ {
		location, locationErr := response.Location()
		response.Body.Close()
		if locationErr != nil || redirects >= 5 || location.Scheme != "https" || location.Host == "" || location.User != nil || location.Fragment != "" {
			return &Error{Code: "github_artifact_redirect_invalid"}
		}
		request, err := http.NewRequestWithContext(ctx, "GET", location.String(), nil)
		if err != nil {
			return &Error{Code: "github_artifact_redirect_invalid"}
		}
		// Storage redirects never receive GitHub Authorization, even when returning to an API host.
		request.Header.Set("User-Agent", "vitryna-publish")
		response, err = c.http.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &Error{Code: "github_artifact_download_failed"}
		}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return &Error{Code: "github_artifact_download_failed", Status: response.StatusCode}
	}
	if response.ContentLength > export.MaxArtifactBytes {
		return &Error{Code: "github_artifact_size_limit"}
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return &Error{Code: "github_artifact_destination_invalid"}
	}
	owned, err := file.Stat()
	if err != nil {
		file.Close()
		return &Error{Code: "github_artifact_write_failed"}
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			if current, err := os.Lstat(destination); err == nil && os.SameFile(owned, current) {
				os.Remove(destination)
			}
		}
	}()
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, export.MaxArtifactBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if copyErr != nil || n > export.MaxArtifactBytes {
		return &Error{Code: "github_artifact_download_failed"}
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return &Error{Code: "github_artifact_transport_digest"}
	}
	if err := file.Sync(); err != nil {
		return &Error{Code: "github_artifact_write_failed"}
	}
	if err := file.Close(); err != nil {
		return &Error{Code: "github_artifact_write_failed"}
	}
	success = true
	return nil
}
