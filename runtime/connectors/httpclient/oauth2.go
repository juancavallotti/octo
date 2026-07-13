// OAuth 2.0 client-credentials support for the http-client connector. A
// tokenSource fetches an access token from the configured token endpoint and
// caches it, refreshing with a refresh_token when the endpoint issued one and
// otherwise re-running the client-credentials grant. The token (and any refresh
// token) is persisted best-effort in the runtime secret store under the system
// namespace, so where the store is durable (the k8s services module) a fresh
// process or replica can adopt a still-valid token instead of minting another.
package httpclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

const (
	// tokenExpirySkew refreshes a token a little before its real expiry, so a
	// request is never sent with an about-to-expire token.
	tokenExpirySkew = 30 * time.Second
	// maxTokenResponseBytes caps how much of a token endpoint response is read.
	maxTokenResponseBytes = 1 << 16 // 64 KiB
)

// oauth2Config is the client-credentials configuration a tokenSource needs.
type oauth2Config struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scope        string // space-joined scopes, empty when none
}

// storedToken is the token envelope cached in memory and persisted in the secret
// store. ExpiresAt is a unix-nanosecond deadline; 0 means unknown, treated as
// expired so the next request refreshes.
type storedToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
}

// tokenResponse is the subset of an OAuth 2.0 token endpoint response we read.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// tokenSource mints and caches access tokens for one http-client connector,
// serializing refreshes so concurrent requests share a single fetch.
type tokenSource struct {
	cfg      oauth2Config
	client   *http.Client
	secrets  core.SecretStore
	storeKey string

	mu     sync.Mutex
	cached storedToken
	loaded bool // whether the cold-start load from the store has been attempted
}

// newTokenSource builds a token source. The store key is derived from the
// connector name and the (client id, token url) pair, so two connectors never
// share a token and the key stays bounded for stores that put it in a URL path.
func newTokenSource(name string, cfg oauth2Config, client *http.Client, secrets core.SecretStore) *tokenSource {
	sum := sha256.Sum256([]byte(cfg.clientID + "|" + cfg.tokenURL))
	return &tokenSource{
		cfg:      cfg,
		client:   client,
		secrets:  secrets,
		storeKey: "oauth2:" + name + ":" + hex.EncodeToString(sum[:]),
	}
}

// Token returns a valid access token, fetching or refreshing one when the cached
// token is missing or about to expire.
func (t *tokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.loaded {
		t.loadFromStore(ctx) // best-effort: adopt a token a prior process persisted
		t.loaded = true
	}

	if t.cached.AccessToken != "" && !expired(t.cached) {
		return t.cached.AccessToken, nil
	}

	tok, err := t.fetch(ctx)
	if err != nil {
		return "", err
	}
	t.cached = tok
	t.persist(ctx, tok) // best-effort
	return tok.AccessToken, nil
}

// expired reports whether tok is at or past its expiry (with the refresh skew). An
// unknown expiry (0) counts as expired so a token of unknown lifetime is refreshed.
func expired(tok storedToken) bool {
	if tok.ExpiresAt == 0 {
		return true
	}
	return time.Now().Add(tokenExpirySkew).UnixNano() >= tok.ExpiresAt
}

// fetch obtains a new token: the refresh_token grant when a refresh token is
// cached, otherwise the client_credentials grant. A failed refresh (expired or
// revoked refresh token) falls back to a fresh client-credentials grant so the
// connector recovers on its own.
func (t *tokenSource) fetch(ctx context.Context) (storedToken, error) {
	if t.cached.RefreshToken != "" {
		tok, err := t.post(ctx, t.refreshForm())
		if err == nil {
			return tok, nil
		}
		return t.post(ctx, t.clientCredentialsForm())
	}
	return t.post(ctx, t.clientCredentialsForm())
}

func (t *tokenSource) clientCredentialsForm() url.Values {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if t.cfg.scope != "" {
		form.Set("scope", t.cfg.scope)
	}
	return form
}

func (t *tokenSource) refreshForm() url.Values {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", t.cached.RefreshToken)
	if t.cfg.scope != "" {
		form.Set("scope", t.cfg.scope)
	}
	return form
}

// post executes a token endpoint request with the given form, authenticating the
// client with HTTP Basic per RFC 6749, and parses the response into a storedToken.
func (t *tokenSource) post(ctx context.Context, form url.Values) (storedToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return storedToken{}, fmt.Errorf("oauth2 build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(t.cfg.clientID, t.cfg.clientSecret)

	resp, err := t.client.Do(req)
	if err != nil {
		return storedToken{}, fmt.Errorf("oauth2 token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return storedToken{}, fmt.Errorf("oauth2 read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return storedToken{}, fmt.Errorf("oauth2 token endpoint returned %d: %s", resp.StatusCode, snippet(body))
	}

	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return storedToken{}, fmt.Errorf("oauth2 decode token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return storedToken{}, errors.New("oauth2 token endpoint returned no access_token")
	}
	return t.newToken(parsed), nil
}

// newToken turns a parsed response into a storedToken, stamping the absolute
// expiry and carrying the previous refresh token forward when the endpoint did
// not return a new one (some servers omit it on refresh).
func (t *tokenSource) newToken(parsed tokenResponse) storedToken {
	tok := storedToken{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = t.cached.RefreshToken
	}
	if parsed.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second).UnixNano()
	}
	return tok
}

// loadFromStore adopts a token previously persisted in the secret store. A miss,
// a no-op store, or a decode error simply leaves the cache empty so Token fetches.
func (t *tokenSource) loadFromStore(ctx context.Context) {
	entry, ok, err := t.secrets.Get(ctx, core.NamespaceSystem, t.storeKey)
	if err != nil || !ok {
		return
	}
	var tok storedToken
	if json.Unmarshal(entry.Value, &tok) == nil {
		t.cached = tok
	}
}

// persist writes the token to the secret store best-effort: it reads the current
// version first so the write lands even if another replica wrote since cold start,
// and ignores a conflict or a no-op store, since the in-memory token already lets
// this process proceed.
func (t *tokenSource) persist(ctx context.Context, tok storedToken) {
	encoded, err := json.Marshal(tok) //nolint:gosec // persisting the token to the secret store is the purpose here
	if err != nil {
		return
	}
	entry, _, err := t.secrets.Get(ctx, core.NamespaceSystem, t.storeKey)
	if err != nil {
		return
	}
	_, _ = t.secrets.Set(ctx, core.NamespaceSystem, t.storeKey, encoded, entry.Version)
}
