// Package httpclient provides a connector that owns a configured net/http client
// for calling external HTTP APIs, plus the "rest" block that runs a single
// request through it and folds the response into the message body.
//
// The connector concentrates client-wide policy: base URL, authentication
// (bearer or basic — OAuth is deferred), default headers applied to every
// request, a request timeout, and an optional in-memory response cache for GETs.
// The rest block references the connector by name and stays thin: it builds a
// request from CEL expressions and hands it to the connector's Do, which resolves
// the URL against the base, layers on auth and default headers, and serves from
// or fills the cache.
package httpclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/connectors/internal/httppool"
	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerREST()
}

func registerConnector() {
	core.MustRegisterConnector("http-client", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: every block/connector in this package
	// falls into the "Integration" palette group with the Globe icon unless it
	// sets its own. The http-client connector and the rest block both inherit.
	core.RegisterExtension(core.ExtensionMeta{Group: "Integration", Icon: "Globe"})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "http-client",
		Label:    "HTTP Client",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const (
	defaultTimeout          = 30 * time.Second
	defaultCacheTTL         = 60 * time.Second
	defaultCacheMaxEntries  = 256
	defaultMaxResponseBytes = 1 << 20 // 1 MiB

	// Retry defaults for 429 (Too Many Requests) responses.
	defaultMaxAttempts = 3
	defaultBaseBackoff = 500 * time.Millisecond
	defaultMaxBackoff  = 30 * time.Second
)

// authType enumerates the supported authentication schemes.
const (
	authNone   = ""
	authBearer = "bearer"
	authBasic  = "basic"
	authOAuth2 = "oauth2" // OAuth 2.0 client-credentials grant
)

// connectorSettings is the client-wide configuration decoded from the
// connector's settings block. Field order matches the editor schema (auth last,
// after maxResponseBytes); Cache carries no octo tag so it stays out of it.
type connectorSettings struct {
	// Prepended to each request path.
	BaseURL string `json:"baseURL" octo:"label=Base URL,required"`
	// Bounds each request.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=30s"`
	// Applied to every request unless already set.
	Headers map[string]string `json:"headers" octo:"label=Headers"`
	// Response body size cap.
	MaxResponseBytes int64 `json:"maxResponseBytes" octo:"label=Max response bytes,default=1048576"`
	// Authentication applied to every request.
	Auth authSettings `json:"auth" octo:"label=Authentication,type=object"`
	// Retry policy for rate-limited (429) responses.
	Retry retrySettings `json:"retry" octo:"label=Retry,type=object"`
	// Connection pool sizing for outbound requests.
	Pool poolSettings `json:"pool" octo:"label=Connection pool,type=object"`
	// Cache enables an in-memory response cache for GET requests (opt-in). It is
	// intentionally untagged: it is not exposed in the editor schema.
	Cache cacheSettings `json:"cache"`
}

// retrySettings configures automatic re-attempts when an upstream returns 429
// Too Many Requests. The Retry-After header is honored when present; otherwise
// the client backs off exponentially. Each wait is capped at MaxBackoff.
type retrySettings struct {
	// Total attempts including the first; <=1 disables retrying.
	MaxAttempts int `json:"maxAttempts" octo:"label=Max attempts,default=3"`
	// Upper bound on each wait between attempts (also caps a large Retry-After).
	MaxBackoff duration `json:"maxBackoff" octo:"label=Max backoff,type=string,default=30s"`
}

// poolSettings sizes the connection pool the client keeps for outbound requests.
// It is the counterpart to a flow's workers: workers asks for N concurrent
// outbound requests, and the pool decides how many of those connections survive
// to serve the next N. Leaving it unset takes defaults sized for that, not Go's
// idle pool of two.
type poolSettings struct {
	// Idle connections kept across all hosts.
	MaxIdleConns int `json:"maxIdleConns" octo:"label=Max idle connections,default=100"`
	// Idle connections kept per host — the one that matters, since a connector
	// talks to a single base URL.
	MaxIdleConnsPerHost int `json:"maxIdleConnsPerHost" octo:"label=Max idle connections per host,default=100"`
	// How long an idle connection is kept before being closed.
	IdleConnTimeout duration `json:"idleConnTimeout" octo:"label=Idle connection timeout,type=string,default=90s"`
	// Disables connection reuse entirely — one connection per request. For an
	// upstream that mishandles persistent connections.
	DisableKeepAlives bool `json:"disableKeepAlives" octo:"label=Disable keep-alives"`
}

// authSettings selects and configures the authentication scheme. Type is "",
// "bearer", "basic", or "oauth2"; the remaining fields are read per scheme and
// shown in the editor only for their scheme (showIf).
type authSettings struct {
	// Authentication scheme.
	Type string `json:"type" octo:"label=Type,type=enum,enum=bearer|basic|oauth2"`
	// Token sent as 'Authorization: Bearer'.
	Token string `json:"token" octo:"label=Bearer token,showIf=type=bearer"`
	// Basic auth username.
	Username string `json:"username" octo:"label=Username,showIf=type=basic"`
	// Basic auth password.
	Password string `json:"password" octo:"label=Password,showIf=type=basic"`

	// OAuth2 token endpoint (client-credentials grant).
	TokenURL string `json:"tokenURL" octo:"label=Token URL,showIf=type=oauth2"`
	// OAuth2 client identifier.
	ClientID string `json:"clientID" octo:"label=Client ID,showIf=type=oauth2"`
	// OAuth2 client secret.
	ClientSecret string `json:"clientSecret" octo:"label=Client secret,showIf=type=oauth2"`
	// Requested OAuth2 scopes.
	Scopes []string `json:"scopes" octo:"label=Scopes,showIf=type=oauth2"`
}

// cacheSettings configures the optional GET response cache.
type cacheSettings struct {
	Enabled    bool     `json:"enabled"`
	TTL        duration `json:"ttl"`        // default 60s
	MaxEntries int      `json:"maxEntries"` // default 256
}

// Connector is a configured HTTP client that rest blocks call through. It is
// safe for concurrent use: *http.Client is, and the cache guards itself.
type Connector struct {
	client   *http.Client
	base     *url.URL
	auth     authSettings
	headers  map[string]string
	maxBytes int64
	cache    *responseCache

	// maxAttempts and maxBackoff bound 429 retry behavior (see Do).
	maxAttempts int
	maxBackoff  time.Duration
	// tokenSource is non-nil only for oauth2 auth; it mints and refreshes the
	// bearer token applied to each request.
	tokenSource *tokenSource
}

// Start parses the settings, validates the base URL and auth, and builds the
// client so a bad configuration fails at startup rather than on first request.
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.BaseURL) == "" {
		return fmt.Errorf("http-client connector %q: baseURL is required", config.Name)
	}
	base, err := url.Parse(set.BaseURL)
	if err != nil {
		return fmt.Errorf("http-client connector %q: baseURL %q: %w", config.Name, set.BaseURL, err)
	}
	if !base.IsAbs() {
		return fmt.Errorf("http-client connector %q: baseURL %q must be absolute (scheme and host)",
			config.Name, set.BaseURL)
	}
	if err := validateAuth(set.Auth); err != nil {
		return fmt.Errorf("http-client connector %q: %w", config.Name, err)
	}

	timeout := time.Duration(set.Timeout)
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	c.client = newPooledClient(set.Pool, timeout)
	c.base = base
	c.auth = set.Auth
	c.headers = set.Headers
	c.maxBytes = set.MaxResponseBytes
	if c.maxBytes <= 0 {
		c.maxBytes = defaultMaxResponseBytes
	}

	c.maxAttempts, c.maxBackoff = resolveRetry(set.Retry)

	if set.Cache.Enabled {
		ttl := time.Duration(set.Cache.TTL)
		if ttl <= 0 {
			ttl = defaultCacheTTL
		}
		maxEntries := set.Cache.MaxEntries
		if maxEntries <= 0 {
			maxEntries = defaultCacheMaxEntries
		}
		c.cache = newResponseCache(ttl, maxEntries)
	}

	if set.Auth.Type == authOAuth2 {
		c.configureOAuth2(ctx, config.Name, set.Auth, timeout)
	}

	slog.Info("http client connector started",
		"connector", config.Name,
		"baseURL", base.Redacted(),
		"auth", authLabel(set.Auth),
		"cache", c.cache != nil,
	)
	return nil
}

// newPooledClient builds the outbound client with an explicit transport. The
// transport is the point: leaving it nil means http.DefaultTransport, whose idle
// pool is two connections per host — see httppool for what that costs.
func newPooledClient(pool poolSettings, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: httppool.New(httppool.Settings{
			MaxIdleConns:        pool.MaxIdleConns,
			MaxIdleConnsPerHost: pool.MaxIdleConnsPerHost,
			IdleConnTimeout:     time.Duration(pool.IdleConnTimeout),
			DisableKeepAlives:   pool.DisableKeepAlives,
		}),
	}
}

// Stop releases idle keep-alive connections held by the client.
func (c *Connector) Stop(context.Context) error {
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
	return nil
}

// Do executes req against the configured client. It resolves req.URL against the
// base URL, applies default headers (without overwriting ones the caller set) and
// authentication, and—for cacheable GETs—serves from or fills the response cache.
// The returned response body is always non-nil and the caller must close it.
func (c *Connector) Do(req *http.Request) (*http.Response, error) {
	if c.client == nil {
		return nil, fmt.Errorf("http-client connector not started")
	}

	req.URL = c.resolveURL(req.URL)
	req.Host = req.URL.Host

	for k, v := range c.headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	if err := c.authorize(req); err != nil {
		return nil, err
	}

	cacheable := c.cache != nil && req.Method == http.MethodGet
	if cacheable {
		if resp, ok := c.cache.get(req.URL.String()); ok {
			return resp, nil
		}
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, err
	}

	if cacheable {
		cached, storeErr := c.cache.store(req.URL.String(), resp, c.maxBytes)
		if storeErr != nil {
			_ = resp.Body.Close()
			return nil, storeErr
		}
		return cached, nil
	}

	// Bound the body for non-cached responses too, so a misbehaving upstream
	// cannot stream an unbounded body into a downstream block.
	resp.Body = newLimitedBody(resp.Body, c.maxBytes)
	return resp, nil
}

// resolveURL builds the absolute request URL from the base URL and the block's
// relative reference, appending the reference path onto the base path (so a base
// with a prefix like "/v1" is preserved) and merging query strings.
func (c *Connector) resolveURL(ref *url.URL) *url.URL {
	final := *c.base
	final.Path = joinPath(c.base.Path, ref.Path)
	// A URL that has a host must carry an absolute path. RequestURI() — what
	// actually goes on the wire — returns the path verbatim, so a relative one
	// sends "GET things HTTP/1.1", which is not a valid origin-form target: the
	// server rejects it at the parser with a bare 400 and no handler ever runs.
	//
	// This is the one place the invariant can be stated, because it is the only
	// one that knows there is a host: Start validates the base is absolute, so
	// every URL resolved here has one. joinPath cannot know — it sees two path
	// strings. And nothing else notices, because String() inserts the missing
	// slash when Host is set, so the log lines and the GET cache key both read
	// perfectly while the request on the wire is malformed.
	if final.Path != "" && !strings.HasPrefix(final.Path, "/") {
		final.Path = "/" + final.Path
	}
	final.RawPath = ""
	switch {
	case c.base.RawQuery == "":
		final.RawQuery = ref.RawQuery
	case ref.RawQuery == "":
		final.RawQuery = c.base.RawQuery
	default:
		final.RawQuery = c.base.RawQuery + "&" + ref.RawQuery
	}
	final.Fragment = ref.Fragment
	return &final
}

// joinPath joins a base path and a reference path with exactly one separating
// slash, preserving a leading slash and the reference's trailing slash.
func joinPath(base, ref string) string {
	switch {
	case ref == "":
		return base
	case base == "":
		return ref
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(ref, "/")
}

// configureOAuth2 builds the token source for client-credentials auth, capturing
// the secret store from the context so refreshed tokens persist across restarts
// where the store is durable.
func (c *Connector) configureOAuth2(ctx context.Context, name string, auth authSettings, timeout time.Duration) {
	secrets := core.RuntimeServicesFromContext(ctx).Secrets()
	c.tokenSource = newTokenSource(name, oauth2Config{
		tokenURL:     auth.TokenURL,
		clientID:     auth.ClientID,
		clientSecret: auth.ClientSecret,
		scope:        strings.Join(auth.Scopes, " "),
	}, httppool.NewClient(timeout), secrets)
}

// authorize applies authentication to the request unless it already carries an
// Authorization header. For oauth2 it mints/refreshes a bearer token (which may
// call the token endpoint and the secret store, hence the error); the static
// schemes never fail.
func (c *Connector) authorize(req *http.Request) error {
	if req.Header.Get("Authorization") != "" {
		return nil
	}
	if c.tokenSource != nil {
		token, err := c.tokenSource.Token(req.Context())
		if err != nil {
			return fmt.Errorf("http-client oauth2: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	c.applyAuth(req)
	return nil
}

// applyAuth sets the Authorization header for the static schemes (bearer, basic).
// The caller has already checked that no Authorization header is present.
func (c *Connector) applyAuth(req *http.Request) {
	switch c.auth.Type {
	case authBearer:
		req.Header.Set("Authorization", "Bearer "+c.auth.Token)
	case authBasic:
		req.SetBasicAuth(c.auth.Username, c.auth.Password)
	}
}

// validateAuth checks that the fields required by the selected auth type are set.
func validateAuth(a authSettings) error {
	switch a.Type {
	case authNone:
		return nil
	case authBearer:
		if a.Token == "" {
			return fmt.Errorf("auth type %q requires a token", a.Type)
		}
	case authBasic:
		if a.Username == "" {
			return fmt.Errorf("auth type %q requires a username", a.Type)
		}
	case authOAuth2:
		if a.TokenURL == "" {
			return fmt.Errorf("auth type %q requires a tokenURL", a.Type)
		}
		if a.ClientID == "" || a.ClientSecret == "" {
			return fmt.Errorf("auth type %q requires a clientID and clientSecret", a.Type)
		}
	default:
		return fmt.Errorf("auth type %q is not one of bearer/basic/oauth2", a.Type)
	}
	return nil
}

// authLabel returns a log-safe label for the configured auth (never the secret).
func authLabel(a authSettings) string {
	if a.Type == authNone {
		return "none"
	}
	return a.Type
}

// duration decodes either a Go duration string ("5s") or a numeric nanosecond
// count from settings, since settings round-trip through JSON.
type duration time.Duration

// UnmarshalJSON parses a duration from a quoted string ("250ms") or a number.
func (d *duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		parsed, err := time.ParseDuration(strings.Trim(s, `"`))
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		*d = duration(parsed)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}
	*d = duration(n)
	return nil
}
