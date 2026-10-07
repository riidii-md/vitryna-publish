package githubapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func appKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestAppJWTSignatureAndClaims(t *testing.T) {
	key, data := appKey(t)
	now := time.Unix(1700000000, 0)
	token, err := appJWT("Iv1_synthetic", data, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("invalid JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != "Iv1_synthetic" || claims.Iat != now.Unix()-60 || claims.Exp != now.Unix()+540 {
		t.Fatal("claims invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], signature); err != nil {
		t.Fatal(err)
	}
	if _, err := appJWT("Iv1_synthetic", []byte("not a private key"), now); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestDedicatedAppInventoryAndTokenLifecycle(t *testing.T) {
	_, key := appKey(t)
	for _, scenario := range []string{"valid", "other installation", "broad inventory", "private target", "unexpected token permission"} {
		t.Run(scenario, func(t *testing.T) {
			var calls []string
			writeMinted := false
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/app":
					return response(200, `{"id":7,"slug":"vitryna-fixture"}`), nil
				case "/app/installations":
					body := `[{"id":11,"app_id":7,"repository_selection":"selected","account":{"login":"example"}}]`
					if scenario == "other installation" {
						body = `[{"id":11,"app_id":7},{"id":12,"app_id":7}]`
					}
					return response(200, body), nil
				case "/users/vitryna-fixture[bot]":
					return response(200, `{"id":21,"login":"vitryna-fixture[bot]","type":"Bot"}`), nil
				case "/app/installations/11/access_tokens":
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					var input struct {
						Repositories []string          `json:"repositories"`
						Permissions  map[string]string `json:"permissions"`
					}
					if err := json.Unmarshal(data, &input); err != nil {
						t.Fatal(err)
					}
					token := "synthetic-read"
					if input.Permissions["contents"] == "write" {
						writeMinted = true
						token = "synthetic-write"
						if !reflect.DeepEqual(input.Repositories, []string{"public"}) || input.Permissions["pull_requests"] != "write" || input.Permissions["workflows"] != "" || input.Permissions["administration"] != "" {
							t.Fatal("write scope invalid")
						}
						if scenario == "unexpected token permission" {
							input.Permissions["administration"] = "write"
						}
					} else if !reflect.DeepEqual(input.Permissions, map[string]string{"metadata": "read"}) || len(input.Repositories) != 0 {
						t.Fatal("inventory token narrowed or overprivileged")
					}
					body, err := json.Marshal(map[string]any{"token": token, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "permissions": input.Permissions, "repository_selection": "selected", "repositories": []map[string]any{{"id": 31, "full_name": "example/public", "private": false}}})
					if err != nil {
						t.Fatal(err)
					}
					return response(201, string(body)), nil
				case "/installation/repositories":
					body := `{"total_count":1,"repositories":[{"id":31,"full_name":"example/public","private":false}]}`
					if scenario == "broad inventory" {
						body = `{"total_count":2,"repositories":[{"id":31,"full_name":"example/public"},{"id":32,"full_name":"example/other"}]}`
					}
					if scenario == "private target" {
						body = `{"total_count":1,"repositories":[{"id":31,"full_name":"example/public","private":true}]}`
					}
					return response(200, body), nil
				case "/installation/token":
					return response(204, ""), nil
				default:
					t.Fatalf("unexpected endpoint: %s", r.URL.Path)
					return nil, nil
				}
			})
			session, err := OpenApp(context.Background(), "Iv1_synthetic", key, "example/public", TokenPermissions{}, &http.Client{Transport: transport})
			if scenario == "valid" {
				if err != nil || session.BotID != 21 || !writeMinted {
					t.Fatalf("open failed: %v", err)
				}
				if session.InstallationToken() != "synthetic-write" {
					t.Fatal("target Git token mismatch")
				}
				if err := session.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				if session.InstallationToken() != "" {
					t.Fatal("revoked token retained")
				}
				if err := session.Close(context.Background()); err != nil {
					t.Fatal("second close not idempotent")
				}
			} else {
				if err == nil {
					session.Close(context.Background())
					t.Fatal("unsafe App accepted")
				}
				if scenario != "unexpected token permission" && writeMinted {
					t.Fatal("write token minted before exact inventory proof")
				}
			}
			if scenario == "valid" || scenario == "unexpected token permission" {
				revocations := 0
				for _, call := range calls {
					if call == "DELETE /installation/token" {
						revocations++
					}
				}
				if revocations != 2 {
					t.Fatalf("tokens not revoked: %#v", calls)
				}
			}
		})
	}
}
