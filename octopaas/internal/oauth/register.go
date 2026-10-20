package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/juancavallotti/octo/octopaas/internal/credentials"
)

// Register introduces octopaas to the provider as a public, native client that
// returns to redirectURI (RFC 7591). There is no secret: the client lives on
// your machine and could not keep one, so every sign-in proves itself with PKCE
// instead.
//
// The registration names the scopes octopaas will ask for: a provider may grant
// a client only what it registered, and refuse a sign-in asking for more.
func Register(ctx context.Context, hc *http.Client, md Metadata, redirectURI string) (credentials.Client, error) {

	payload, err := json.Marshal(map[string]any{
		"client_name":                "octopaas",
		"application_type":           "native",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      strings.Join(Scopes(md), " "),
	})
	if err != nil {
		return credentials.Client{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, md.RegistrationEndpoint, bytes.NewReader(payload))
	if err != nil {
		return credentials.Client{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return credentials.Client{}, fmt.Errorf("register with %s: %w", md.Issuer, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return credentials.Client{}, fmt.Errorf("%s would not register octopaas (%d): %s", md.Issuer, resp.StatusCode, oauthError(body))
	}
	var registered struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(body, &registered); err != nil || registered.ClientID == "" {
		return credentials.Client{}, fmt.Errorf("%s registered octopaas but sent back no client_id", md.Issuer)
	}
	return credentials.Client{ClientID: registered.ClientID, RedirectURI: redirectURI, Scope: strings.Join(Scopes(md), " "), RegisteredAt: time.Now().UTC()}, nil
}

// oauthError reads RFC 6749's {"error", "error_description"}, or the body's
// first bytes when it is something else.
func oauthError(body []byte) string {

	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		if e.Description != "" {
			return e.Error + ": " + e.Description
		}
		return e.Error
	}
	if len(body) > 200 {
		body = body[:200]
	}
	return string(bytes.TrimSpace(body))
}
