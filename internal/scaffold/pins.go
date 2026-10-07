package scaffold

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/riidii-md/vitryna-publish/export"
)

type ReleasePins struct {
	Version        string            `json:"version"`
	ArtifactFormat string            `json:"artifactFormat"`
	Binaries       map[string]string `json:"binaries"`
}

func pinError() error { return &export.Diagnostic{Code: "release_pins_invalid"} }

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 8 {
		return pinError()
	}
	token, err := d.Token()
	if err != nil {
		return pinError()
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delimiter == '{' {
			key, err := d.Token()
			if err != nil {
				return pinError()
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return pinError()
			}
			seen[name] = true
		}
		if err := uniqueJSON(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	if err != nil {
		return pinError()
	}
	return nil
}

func DecodePins(data []byte, version string) (ReleasePins, error) {
	var pins ReleasePins
	if len(data) > 64<<10 || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`).MatchString(version) {
		return pins, pinError()
	}
	if err := uniqueJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return pins, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&pins); err != nil {
		return pins, pinError()
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return pins, pinError()
	}
	if pins.Version != version || pins.ArtifactFormat != "github-zip-bundle-v1" || len(pins.Binaries) != 4 {
		return pins, pinError()
	}
	for _, platform := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"} {
		value := pins.Binaries[platform]
		if len(value) != 64 || value != strings.ToLower(value) {
			return pins, pinError()
		}
		if _, err := hex.DecodeString(value); err != nil {
			return pins, pinError()
		}
	}
	return pins, nil
}

func FetchPins(ctx context.Context, version string) (ReleasePins, error) {
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`).MatchString(version) {
		return ReleasePins{}, pinError()
	}
	request, err := http.NewRequestWithContext(ctx, "GET", "https://github.com/riidii-md/vitryna-publish/releases/download/"+version+"/vitryna-pins.json", nil)
	if err != nil {
		return ReleasePins{}, pinError()
	}
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || r.URL.Scheme != "https" || r.URL.User != nil {
			return pinError()
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		return ReleasePins{}, &export.Diagnostic{Code: "release_pins_unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ReleasePins{}, &export.Diagnostic{Code: "release_pins_unavailable"}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil {
		return ReleasePins{}, pinError()
	}
	return DecodePins(data, version)
}
