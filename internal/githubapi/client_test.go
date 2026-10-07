package githubapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientIdentityAndRedaction(t *testing.T) {
	client := New("synthetic-secret", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("X-GitHub-Api-Version") != APIVersion {
			t.Fatal("request identity invalid")
		}
		return response(403, `{"message":"synthetic-secret private-response"}`), nil
	})})
	var out any
	err := client.DoJSON(context.Background(), "GET", "/app", nil, &out)
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "private-response") {
		t.Fatalf("unsafe error: %v", err)
	}
	var e *Error
	if !errors.As(err, &e) || e.Status != 403 {
		t.Fatalf("missing typed failure: %v", err)
	}
}

func TestReadRetriesButWriteDoesNot(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		calls := 0
		client := New("", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(503, "private failure"), nil
			}
			return response(200, `{"id":1}`), nil
		})})
		var out struct {
			ID int `json:"id"`
		}
		err := client.DoJSON(context.Background(), method, "/app", nil, &out)
		if method == "GET" {
			if err != nil || calls != 2 || out.ID != 1 {
				t.Fatalf("read retry: %d %v", calls, err)
			}
		} else if err == nil || calls != 1 {
			t.Fatalf("write blindly retried: %d %v", calls, err)
		}
	}
}

func TestClientRefusesRedirectAndUnboundedJSON(t *testing.T) {
	for _, scenario := range []string{"redirect", "oversize", "trailing"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			client := New("fake", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if scenario == "redirect" {
					out := response(302, "")
					out.Header.Set("Location", "https://storage.example.invalid/signed")
					return out, nil
				}
				if scenario == "oversize" {
					return response(200, strings.Repeat(" ", int(MaxJSONBytes)+1)), nil
				}
				return response(200, `{"id":1} {"id":2}`), nil
			})})
			var out any
			if err := client.DoJSON(context.Background(), "GET", "/app", nil, &out); err == nil || calls != 1 {
				t.Fatalf("unsafe response accepted: %d %v", calls, err)
			}
		})
	}
}
