package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestCanceledCallerTokenCleanup(t *testing.T) {
	_, key := appKey(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writeRevoked := false
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		// A real transport cannot complete a request under a canceled context.
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		switch r.URL.Path {
		case "/app":
			return response(200, `{"id":7,"slug":"vitryna-fixture"}`), nil
		case "/app/installations":
			return response(200, `[{"id":11,"app_id":7,"repository_selection":"selected","account":{"login":"example"}}]`), nil
		case "/users/vitryna-fixture[bot]":
			return response(200, `{"id":21,"login":"vitryna-fixture[bot]","type":"Bot"}`), nil
		case "/app/installations/11/access_tokens":
			var input struct {
				Permissions map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			token := "synthetic-inventory"
			if input.Permissions["contents"] == "write" {
				token = "synthetic-write"
			}
			body, err := json.Marshal(map[string]any{
				"token":                token,
				"expires_at":           time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				"permissions":          input.Permissions,
				"repository_selection": "selected",
				"repositories":         []map[string]any{{"id": 31, "full_name": "example/public", "private": false}},
			})
			if err != nil {
				t.Fatal(err)
			}
			return response(201, string(body)), nil
		case "/installation/repositories":
			return response(200, `{"total_count":1,"repositories":[{"id":31,"full_name":"example/public","private":false}]}`), nil
		case "/installation/token":
			if r.Method != "DELETE" {
				t.Fatalf("cleanup used %s", r.Method)
			}
			deadline, ok := r.Context().Deadline()
			if !ok || !deadline.After(time.Now()) || time.Until(deadline) > 10*time.Second {
				t.Fatal("cleanup lacks a live bounded deadline")
			}
			if r.Header.Get("Authorization") == "Bearer synthetic-write" {
				if ctx.Err() != context.Canceled {
					t.Fatal("test did not cancel owner before cleanup")
				}
				writeRevoked = true
			}
			return response(204, ""), nil
		default:
			return nil, fmt.Errorf("unexpected synthetic endpoint %s", r.URL.Path)
		}
	})
	session, err := OpenApp(ctx, "Iv1_synthetic", key, "example/public", TokenPermissions{}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if session.InstallationToken() != "synthetic-write" {
		t.Fatal("write token not issued")
	}
	cancel()
	if err := session.Close(ctx); err != nil {
		t.Fatalf("canceled owner prevented token compensation: %v", err)
	}
	if !writeRevoked {
		t.Fatal("issued write token not revoked")
	}
	if session.InstallationToken() != "" || session.Client.token != "" {
		t.Fatal("revoked authorization retained")
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("second close not idempotent: %v", err)
	}
}
