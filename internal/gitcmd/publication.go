package gitcmd

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os/exec"
	"regexp"
)

var ErrPublicationGit = errors.New("target_git_failed: inspect target permissions, base movement, and the explicit promotion lease")

type Publication struct{ repository, remote, token, name, email string }

func NewPublication(repository, remote, token, name, email string) *Publication {
	return &Publication{repository: repository, remote: remote, token: token, name: name, email: email}
}

func (p *Publication) Read(ctx context.Context, maxBytes int64, input io.Reader, args ...string) ([]byte, error) {
	if maxBytes < 0 || p.token != "" && !regexp.MustCompile(`^https://github\.com/[A-Za-z0-9-]+/[A-Za-z0-9_.-]+\.git$`).MatchString(p.remote) {
		return nil, ErrPublicationGit
	}
	argv := []string{"--no-replace-objects", "--no-lazy-fetch", "-c", "core.hooksPath=/dev/null", "-c", "core.autocrlf=false", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "http.followRedirects=false", "-C", p.repository}
	cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
	cmd.Env = baseEnvironment()
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME="+p.name, "GIT_AUTHOR_EMAIL="+p.email, "GIT_COMMITTER_NAME="+p.name, "GIT_COMMITTER_EMAIL="+p.email)
	if p.token != "" {
		header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+p.token))
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http."+p.remote+".extraHeader", "GIT_CONFIG_VALUE_0="+header)
	}
	buffer := limitedBuffer{left: maxBytes}
	cmd.Stdout = &buffer
	cmd.Stderr = io.Discard
	cmd.Stdin = input
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrPublicationGit
	}
	return buffer.buffer.Bytes(), nil
}
