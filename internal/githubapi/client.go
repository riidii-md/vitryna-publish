package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Reviewed against https://docs.github.com/en/rest/about-the-rest-api/api-versions.
const APIVersion = "2026-03-10"
const MaxJSONBytes int64 = 4 << 20

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s (HTTP %d)", e.Code, e.Status)
	}
	return e.Code
}

type Client struct {
	http  *http.Client
	token string
}

func New(token string, httpClient *http.Client) *Client {
	var client http.Client
	if httpClient != nil {
		client = *httpClient
	}
	if client.Timeout <= 0 || client.Timeout > 45*time.Second {
		client.Timeout = 45 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &client, token: token}
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\r\n#") {
		return nil, &Error{Code: "github_endpoint_invalid"}
	}
	r, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Code: "github_request_invalid"}
	}
	r.Header.Set("Accept", "application/vnd.github+json")
	r.Header.Set("X-GitHub-Api-Version", APIVersion)
	r.Header.Set("User-Agent", "vitryna-publish")
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	if len(body) > 0 {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		code := "github_transport_failed"
		if method != "GET" && method != "HEAD" {
			code = "github_ambiguous_write"
		}
		return nil, &Error{Code: code}
	}
	return response, nil
}

func (c *Client) DoJSON(ctx context.Context, method, path string, input, output any) error {
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil || int64(len(body)) > MaxJSONBytes {
			return &Error{Code: "github_request_invalid"}
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		response, err := c.request(ctx, method, path, body)
		if err != nil {
			return err
		}
		if (method == "GET" || method == "HEAD") && attempt < 2 && (response.StatusCode == 429 || response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504) {
			response.Body.Close()
			timer := time.NewTimer(time.Duration(attempt+1) * 200 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return &Error{Code: "github_http_failed", Status: response.StatusCode}
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, MaxJSONBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return &Error{Code: "github_response_failed"}
		}
		if int64(len(data)) > MaxJSONBytes {
			return &Error{Code: "github_response_limit"}
		}
		if output == nil {
			return nil
		}
		d := json.NewDecoder(bytes.NewReader(data))
		if err := d.Decode(output); err != nil {
			return &Error{Code: "github_response_invalid"}
		}
		var trailing any
		if err := d.Decode(&trailing); err != io.EOF {
			return &Error{Code: "github_response_invalid"}
		}
		return nil
	}
	return &Error{Code: "github_retry_exhausted"}
}
