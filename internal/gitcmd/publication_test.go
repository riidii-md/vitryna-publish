package gitcmd

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationCredentialsScopedAndNotInherited(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$VITRYNA_APP_PRIVATE_KEY\" \"$GITHUB_TOKEN\" \"$GIT_AUTHOR_NAME\" \"$GIT_AUTHOR_EMAIL\" \"$GIT_CONFIG_KEY_0\" \"$GIT_CONFIG_VALUE_0\"\nprintf '%s\\n' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("VITRYNA_APP_PRIVATE_KEY", "synthetic-root-key")
	t.Setenv("GITHUB_TOKEN", "synthetic-source-token")
	profile := NewPublication(dir, "https://github.com/example/public.git", "synthetic-target-token", "fixture[bot]", "7+fixture[bot]@users.noreply.github.com")
	data, err := profile.Read(context.Background(), 4096, nil, "status")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 7 || lines[0] != "" || lines[1] != "" || lines[2] != "fixture[bot]" || lines[3] != "7+fixture[bot]@users.noreply.github.com" || lines[4] != "http.https://github.com/example/public.git.extraHeader" || lines[5] != "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:synthetic-target-token")) {
		t.Fatal("publication credential profile invalid")
	}
	if strings.Contains(strings.Join(lines[6:], "\n"), "synthetic-target-token") {
		t.Fatal("target token exposed in Git arguments")
	}
}
