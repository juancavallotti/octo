// Google Cloud workload identity for the http-client connector. A gcpTokenSource
// asks the instance metadata server for a token minted for the service account
// this process runs as, and caches it until it is nearly expired.
//
// It mints one of two things. An *identity* token is an OIDC JWT stamped with an
// audience, and it is what one Cloud Run service presents to another, or to an
// IAP-protected endpoint. An *access* token is an OAuth token carrying scopes,
// and it is what a call to a Google API — Storage, Pub/Sub, BigQuery — wants.
//
// Unlike oauth2.go this persists nothing. That file caches its token in the
// runtime secret store so a replica can adopt one another process already paid
// for; here there is nothing to save. The metadata server is local, free, and
// always available to a process that is entitled to a token at all, and a token
// minted for this instance's identity is not something to hand around between
// replicas through shared storage.
package httpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
)

// Which token the metadata server is asked for.
const (
	gcpTokenIdentity = "identity"
	gcpTokenAccess   = "access"
)

// The metadata paths behind each. The client supplies the host and the
// Metadata-Flavor header, so these are the suffixes only.
const (
	gcpIdentityPath = "instance/service-accounts/default/identity"
	gcpAccessPath   = "instance/service-accounts/default/token"
)

// gcpFallbackTTL is how long an identity token is trusted when its expiry cannot
// be read. Google mints them for an hour; assuming rather less costs an extra
// fetch and never an expired token on the wire.
const gcpFallbackTTL = 30 * time.Minute

// jwtSegments is how many dot-separated parts a JWT has. Only the middle one is
// read here — see identityExpiry for why it is not verified.
const jwtSegments = 3

// errOffGCE explains the failure a laptop sees, which is otherwise reported as an
// opaque dial timeout against a hostname nobody recognises.
var errOffGCE = errors.New(
	"metadata server unreachable (this runtime is not running on Google Cloud; " +
		"gcp auth requires Cloud Run, GCE, GKE, or another metadata-server environment)")

// gcpConfig is what a gcpTokenSource needs, resolved from settings at startup.
type gcpConfig struct {
	tokenKind string
	audience  string        // identity only
	scopes    string        // access only, comma-joined
	timeout   time.Duration // the connector's timeout, applied to the fetch
}

// gcpTokenSource mints and caches tokens for one connector, serializing fetches
// so concurrent requests share a single call to the metadata server.
type gcpTokenSource struct {
	cfg gcpConfig

	mu     sync.Mutex
	cached storedToken
}

// newGCPTokenSource builds a token source for the resolved configuration.
func newGCPTokenSource(cfg gcpConfig) *gcpTokenSource {
	return &gcpTokenSource{cfg: cfg}
}

// Token returns a valid token, fetching one when the cached token is missing or
// close to expiry. It reuses the same skew oauth2 does, so a request is never
// sent with a token that is about to lapse in flight.
func (g *gcpTokenSource) Token(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cached.AccessToken != "" && !expired(g.cached) {
		return g.cached.AccessToken, nil
	}
	token, err := g.fetch(ctx)
	if err != nil {
		return "", err
	}
	g.cached = token
	return token.AccessToken, nil
}

// fetch asks the metadata server for whichever token this source was configured
// for, bounded by the connector's timeout.
//
// The bound matters even though the metadata server is link-local: without it a
// hung metadata call holds a flow worker for as long as the caller's context
// allows, which is the flow's deadline rather than this connector's. The oauth2
// source gets the same bound from the http.Client it was built with; this one has
// no client of its own, since the metadata package brings its own.
//
// WithTimeout never extends an existing deadline, so a caller already closer to
// giving up still wins.
func (g *gcpTokenSource) fetch(ctx context.Context) (storedToken, error) {
	if g.cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.cfg.timeout)
		defer cancel()
	}
	if g.cfg.tokenKind == gcpTokenAccess {
		return g.fetchAccess(ctx)
	}
	return g.fetchIdentity(ctx)
}

// fetchIdentity gets an OIDC token for the configured audience. format=full asks
// for the full identity, which is what a Cloud Run receiver expects.
func (g *gcpTokenSource) fetchIdentity(ctx context.Context) (storedToken, error) {
	path := gcpIdentityPath + "?format=full&audience=" + url.QueryEscape(g.cfg.audience)
	token, err := metadata.GetWithContext(ctx, path)
	if err != nil {
		return storedToken{}, metadataError(err)
	}
	if token == "" {
		return storedToken{}, errors.New("metadata server returned an empty identity token")
	}
	return storedToken{AccessToken: token, ExpiresAt: identityExpiry(token)}, nil
}

// fetchAccess gets an OAuth access token, optionally narrowed to scopes. With no
// scopes the metadata server returns the service account's own, which on Cloud
// Run is cloud-platform.
func (g *gcpTokenSource) fetchAccess(ctx context.Context) (storedToken, error) {
	path := gcpAccessPath
	if g.cfg.scopes != "" {
		path += "?scopes=" + url.QueryEscape(g.cfg.scopes)
	}
	raw, err := metadata.GetWithContext(ctx, path)
	if err != nil {
		return storedToken{}, metadataError(err)
	}
	var parsed tokenResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return storedToken{}, fmt.Errorf("decode metadata token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return storedToken{}, errors.New("metadata server returned no access_token")
	}
	token := storedToken{AccessToken: parsed.AccessToken}
	if parsed.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second).UnixNano()
	}
	return token, nil
}

// identityExpiry reads the exp claim out of an identity token, falling back to a
// conservative lifetime when it cannot.
//
// The signature is deliberately not verified. This token was just minted by the
// metadata server for this process; there is no untrusted party between the two,
// and the claim is being read to schedule a refresh rather than to make an
// authorization decision. Verifying it is the receiving service's job.
func identityExpiry(token string) int64 {
	segments := strings.Split(token, ".")
	if len(segments) != jwtSegments {
		return fallbackExpiry()
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return fallbackExpiry()
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return fallbackExpiry()
	}
	return time.Unix(claims.Exp, 0).UnixNano()
}

// fallbackExpiry is the deadline used when a token's own is unreadable.
func fallbackExpiry() int64 { return time.Now().Add(gcpFallbackTTL).UnixNano() }

// metadataError explains an unreachable metadata server rather than passing the
// transport error through. Off GCE that error names a hostname the reader has
// never configured, which sends them looking in the wrong place.
func metadataError(err error) error {
	if !metadata.OnGCE() {
		return fmt.Errorf("%w: %w", errOffGCE, err)
	}
	return fmt.Errorf("metadata server: %w", err)
}

// configureGCP builds the token source from settings.
//
// An empty audience defaults to the connector's base URL, origin only. That is
// the audience a Cloud Run service validates against, so the common case —
// calling one service from another — needs no audience configured at all.
func (c *Connector) configureGCP(auth authSettings, base *url.URL, timeout time.Duration) {
	kind := auth.GCPToken
	if kind == "" {
		kind = gcpTokenIdentity
	}
	audience := auth.GCPAudience
	if audience == "" && base != nil {
		audience = (&url.URL{Scheme: base.Scheme, Host: base.Host}).String()
	}
	c.tokens = newGCPTokenSource(gcpConfig{
		tokenKind: kind,
		audience:  audience,
		scopes:    strings.Join(auth.GCPScopes, ","),
		timeout:   timeout,
	})
}

// validateGCPToken rejects a token kind that is neither of the two supported, so
// a typo fails at startup rather than becoming a surprising identity token.
func validateGCPToken(a authSettings) error {
	switch a.GCPToken {
	case "", gcpTokenIdentity, gcpTokenAccess:
		return nil
	default:
		return fmt.Errorf("auth type %q requires gcpToken to be %q or %q, got %q",
			a.Type, gcpTokenIdentity, gcpTokenAccess, a.GCPToken)
	}
}
