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
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type TokenPermissions struct{ Workflows, ClassicProtection, ReadOnly bool }
type AppSession struct {
	Client    *Client
	BotID     int64
	BotLogin  string
	ExpiresAt time.Time
	token     string
}

type repositoryIdentity struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  *bool  `json:"private"`
}

type installationToken struct {
	Token               string               `json:"token"`
	ExpiresAt           time.Time            `json:"expires_at"`
	Permissions         map[string]string    `json:"permissions"`
	RepositorySelection string               `json:"repository_selection"`
	Repositories        []repositoryIdentity `json:"repositories"`
}

func appJWT(clientID string, keyPEM []byte, now time.Time) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_]{1,128}$`).MatchString(clientID) || len(keyPEM) > 64<<10 {
		return "", &Error{Code: "github_app_credentials_invalid"}
	}
	block, rest := pem.Decode(keyPEM)
	if block == nil || strings.TrimSpace(string(rest)) != "" {
		return "", &Error{Code: "github_app_key_invalid"}
	}
	var key *rsa.PrivateKey
	var err error
	if block.Type == "RSA PRIVATE KEY" {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		var value any
		value, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = value.(*rsa.PrivateKey)
	} else {
		return "", &Error{Code: "github_app_key_invalid"}
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return "", &Error{Code: "github_app_key_invalid"}
	}
	payload, _ := json.Marshal(struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}{now.Unix() - 60, now.Unix() + 540, clientID})
	message := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload)
	hash := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", &Error{Code: "github_app_sign_failed"}
	}
	return message + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func permissionsValid(actual, wanted map[string]string) bool {
	for key, value := range actual {
		if key == "metadata" && value == "read" {
			continue
		}
		if wanted[key] != value {
			return false
		}
	}
	for key, value := range wanted {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func revoke(client *Client, ctx context.Context) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return client.DoJSON(cleanup, "DELETE", "/installation/token", nil, nil)
}

func (s *AppSession) InstallationToken() string { return s.token }

func (s *AppSession) Close(ctx context.Context) error {
	if s.token == "" {
		return nil
	}
	if err := revoke(s.Client, ctx); err != nil {
		return err
	}
	s.token, s.Client.token = "", ""
	return nil
}

func OpenApp(ctx context.Context, clientID string, keyPEM []byte, target string, scope TokenPermissions, httpClient *http.Client) (*AppSession, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,99}/[A-Za-z0-9_.-]{1,100}$`).MatchString(target) {
		return nil, &Error{Code: "github_target_invalid"}
	}
	owner, name, _ := strings.Cut(target, "/")
	jwt, err := appJWT(clientID, keyPEM, time.Now())
	if err != nil {
		return nil, err
	}
	appClient := New(jwt, httpClient)
	var app struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	if err := appClient.DoJSON(ctx, "GET", "/app", nil, &app); err != nil {
		return nil, err
	}
	if app.ID <= 0 || !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(app.Slug) {
		return nil, &Error{Code: "github_app_identity_invalid"}
	}
	var installations []struct {
		ID          int64      `json:"id"`
		AppID       int64      `json:"app_id"`
		Selection   string     `json:"repository_selection"`
		SuspendedAt *time.Time `json:"suspended_at"`
		Account     struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := appClient.DoJSON(ctx, "GET", "/app/installations?per_page=2", nil, &installations); err != nil {
		return nil, err
	}
	if len(installations) != 1 || installations[0].ID <= 0 || installations[0].AppID != app.ID || installations[0].Account.Login != owner || installations[0].Selection != "selected" || installations[0].SuspendedAt != nil {
		return nil, &Error{Code: "github_app_installation_scope"}
	}
	endpoint := "/app/installations/" + strconv.FormatInt(installations[0].ID, 10) + "/access_tokens"
	// An unrestricted-repository metadata-only token proves the actual inventory before any write token exists.
	readPermissions := map[string]string{"metadata": "read"}
	var inventoryToken installationToken
	if err := appClient.DoJSON(ctx, "POST", endpoint, map[string]any{"permissions": readPermissions}, &inventoryToken); err != nil {
		return nil, err
	}
	if inventoryToken.Token == "" {
		return nil, &Error{Code: "github_token_invalid"}
	}
	inventoryClient := New(inventoryToken.Token, httpClient)
	var targetID int64
	inventoryErr := func() error {
		if !permissionsValid(inventoryToken.Permissions, readPermissions) || !tokenExpiryValid(inventoryToken.ExpiresAt) {
			return &Error{Code: "github_token_scope"}
		}
		var inventory struct {
			Total        int                  `json:"total_count"`
			Repositories []repositoryIdentity `json:"repositories"`
		}
		if err := inventoryClient.DoJSON(ctx, "GET", "/installation/repositories?per_page=2", nil, &inventory); err != nil {
			return err
		}
		if inventory.Total != 1 || len(inventory.Repositories) != 1 || inventory.Repositories[0].ID <= 0 || inventory.Repositories[0].FullName != target || inventory.Repositories[0].Private == nil || *inventory.Repositories[0].Private {
			return &Error{Code: "github_app_repository_scope"}
		}
		targetID = inventory.Repositories[0].ID
		return nil
	}()
	revokeErr := revoke(inventoryClient, ctx)
	if inventoryErr != nil {
		return nil, inventoryErr
	}
	if revokeErr != nil {
		return nil, revokeErr
	}
	var bot struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := New("", httpClient).DoJSON(ctx, "GET", "/users/"+url.PathEscape(app.Slug+"[bot]"), nil, &bot); err != nil {
		return nil, err
	}
	if bot.ID <= 0 || bot.Login != app.Slug+"[bot]" || bot.Type != "Bot" {
		return nil, &Error{Code: "github_app_bot_invalid"}
	}
	wanted := map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}
	if scope.ReadOnly {
		wanted["contents"] = "read"
		delete(wanted, "pull_requests")
	}
	if scope.Workflows && !scope.ReadOnly {
		wanted["workflows"] = "write"
	}
	if scope.ClassicProtection {
		wanted["administration"] = "read"
	}
	var token installationToken
	if err := appClient.DoJSON(ctx, "POST", endpoint, map[string]any{"repositories": []string{name}, "permissions": wanted}, &token); err != nil {
		return nil, err
	}
	if token.Token == "" {
		return nil, &Error{Code: "github_token_invalid"}
	}
	client := New(token.Token, httpClient)
	if !permissionsValid(token.Permissions, wanted) || !tokenExpiryValid(token.ExpiresAt) || token.RepositorySelection != "selected" || len(token.Repositories) != 1 || token.Repositories[0].ID != targetID || token.Repositories[0].FullName != target || token.Repositories[0].Private == nil || *token.Repositories[0].Private {
		if err := revoke(client, ctx); err != nil {
			return nil, err
		}
		return nil, &Error{Code: "github_token_scope"}
	}
	return &AppSession{Client: client, BotID: bot.ID, BotLogin: bot.Login, ExpiresAt: token.ExpiresAt, token: token.Token}, nil
}

func tokenExpiryValid(expiry time.Time) bool {
	now := time.Now()
	return expiry.After(now.Add(time.Minute)) && !expiry.After(now.Add(time.Hour+time.Minute))
}
