package scaffold

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixturePins() ReleasePins {
	return ReleasePins{Version: "v0.1.0-fixture", ArtifactFormat: "github-zip-bundle-v1", Binaries: map[string]string{"linux-amd64": strings.Repeat("a", 64), "linux-arm64": strings.Repeat("b", 64), "darwin-amd64": strings.Repeat("c", 64), "darwin-arm64": strings.Repeat("d", 64)}}
}

func TestReleasePinsStrictAndVersionBound(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong version", "unknown field", "missing platform", "wrong format", "invalid hash", "duplicate field"} {
		t.Run(scenario, func(t *testing.T) {
			pins := fixturePins()
			if scenario == "missing platform" {
				delete(pins.Binaries, "linux-arm64")
			}
			if scenario == "wrong format" {
				pins.ArtifactFormat = "raw-tar"
			}
			if scenario == "invalid hash" {
				pins.Binaries["linux-amd64"] = "bad"
			}
			data, err := json.Marshal(pins)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unknown field" {
				data = []byte(strings.TrimSuffix(string(data), "}") + `,"unexpected":true}`)
			}
			if scenario == "duplicate field" {
				data = []byte(strings.TrimSuffix(string(data), "}") + `,"version":"v0.1.0-fixture"}`)
			}
			version := pins.Version
			if scenario == "wrong version" {
				version = "v0.2.0"
			}
			_, err = DecodePins(data, version)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("pins %v", err)
			}
		})
	}
}
