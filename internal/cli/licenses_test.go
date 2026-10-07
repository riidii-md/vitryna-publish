package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestLicensesPrintsCompletePinnedNotices(t *testing.T) {
	var out, errs bytes.Buffer
	if exit := Run(context.Background(), []string{"licenses"}, &out, &errs, "test"); exit != 0 || errs.Len() != 0 {
		t.Fatalf("legal material unavailable: %d %s", exit, errs.String())
	}
	expected := map[string]string{
		"Vitryna Publish (MIT)":           "ec85364056dca7cd736ca782f2cb8eae1a5cbcd962326a8e16cc640989116a56",
		"Adapted Tatami algorithms (MIT)": "0e7fa23ad0fab7ec6c46b992915e1ddb16a84eb634ea20b3e51a0707d1155db2",
		"Go runtime/standard library and golang.org/x/text v0.32.0 (BSD-3-Clause)": "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
		"Go and golang.org/x/text patent grant":                                    "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
		"go.yaml.in/yaml/v3 v3.0.5 LICENSE (MIT and Apache-2.0)":                   "d18f6323b71b0b768bb5e9616e36da390fbd39369a81807cca352de4e4e6aa0b",
		"go.yaml.in/yaml/v3 v3.0.5 NOTICE":                                         "f6c2dd3a67b576eafb89b80200b8b1627230bf3821a0c14cb99a22ac19107d00",
		"Apache License 2.0 (YAML)":                                                "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30",
	}
	parts := strings.Split(out.String(), "=== ")
	if len(parts) != len(expected)+1 {
		t.Fatalf("legal component count: %d", len(parts)-1)
	}
	for _, part := range parts[1:] {
		name, body, ok := strings.Cut(part, " ===\n")
		hash := sha256.Sum256([]byte(body))
		if !ok || expected[name] == "" || hex.EncodeToString(hash[:]) != expected[name] {
			t.Fatalf("incomplete or changed notice: %q", name)
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		t.Fatalf("missing notices: %#v", expected)
	}
	out.Reset()
	errs.Reset()
	if exit := Run(context.Background(), []string{"help"}, &out, &errs, "test"); exit != 0 || !strings.Contains(out.String(), "licenses") {
		t.Fatal("notices not discoverable")
	}
	out.Reset()
	errs.Reset()
	if exit := Run(context.Background(), []string{"licenses", "unexpected"}, &out, &errs, "test"); exit != 2 || out.Len() != 0 {
		t.Fatal("licenses accepted extra arguments")
	}
}
