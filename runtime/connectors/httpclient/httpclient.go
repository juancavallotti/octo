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
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterConnector("http-client", func() core.Connector {
		return &Connector{}
	})
}

const (
	defaultTimeout          = 30 * time.Second
	defaultCacheTTL         = 60 * time.Second
	defaultCacheMaxEntries  = 256
	defaultMaxResponseBytes = 1 << 20 // 1 MiB
)

// authType enumerates the supported authentication schemes.
const (
	authNone   = ""
	authBearer = "bearer"
	authBasic  = "basic"
)

// connectorSettings is the client-wide configuration decoded from the
// connector's settings block.
type connectorSettings struct {
	// BaseURL is prepended to each request path (required), e.g.
	// "https://api.example.com" or "https://api.example.com/v1".
	BaseURL string `json:"baseURL"`
	// Timeout bounds each request (default 30s). Accepts a duration string
	// ("10s") or a nanosecond count.
	Timeout duration `json:"timeout"`
	// Headers are applied to every request unless the request already sets them.
	Headers map[string]string `json:"headers"`
	// Auth configures bearer or basic authentication (optional).
	Auth authSettings `json:"auth"`
	// Cache enables an in-memory response cache for GET requests (opt-in).
	Cache cacheSettings `json:"cache"`
	// MaxResponseBytes caps how much of a response body is read (default 1 MiB).
	MaxResponseBytes int64 `json:"maxResponseBytes"`
}

// authSettings selects and configures the authentication scheme. Type is "",
// "bearer", or "basic"; the remaining fields are read per scheme.
type authSettings struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
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
}

// Start parses the settings, validates the base URL and auth, and builds the
// client so a bad configuration fails at startup rather than on first request.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
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
	c.client = &http.Client{Timeout: timeout}
	c.base = base
	c.auth = set.Auth
	c.headers = set.Headers
	c.maxBytes = set.MaxResponseBytes
	if c.maxBytes <= 0 {
		c.maxBytes = defaultMaxResponseBytes
	}

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

	slog.Info("http client connector started",
		"connector", config.Name,
		"baseURL", base.Redacted(),
		"auth", authLabel(set.Auth),
		"cache", c.cache != nil,
	)
	return nil
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
	c.applyAuth(req)

	cacheable := c.cache != nil && req.Method == http.MethodGet
	if cacheable {
		if resp, ok := c.cache.get(req.URL.String()); ok {
			return resp, nil
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http-client do %s %s: %w", req.Method, req.URL.Redacted(), err)
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

// applyAuth sets the Authorization header per the configured scheme, unless the
// request already carries one.
func (c *Connector) applyAuth(req *http.Request) {
	if req.Header.Get("Authorization") != "" {
		return
	}
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
	default:
		return fmt.Errorf("auth type %q is not one of bearer/basic", a.Type)
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
