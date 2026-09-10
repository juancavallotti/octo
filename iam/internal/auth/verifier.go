// Package auth is the token exchange: it takes the bearer token an identity
// provider issued at sign-in, verifies it, resolves the caller to a platform user
// with roles, and mints an internal token signed by this service.
//
// It is the only place in the platform that talks to an identity provider from
// Go. Everything downstream — the orchestrator, the observability service, the
// runtime's own jwt-validate block — verifies the token this package produces,
// against keys iam publishes, and so needs to know nothing about OIDC at all.
// That is the whole point of the exchange: one component understands the
// provider, and it converts what the provider says into something the rest of the
// system can check for itself.
package auth

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Identity is what an identity provider told us about the caller. It is
// deliberately three fields: the subject that keys the user row, and the two
// pieces of profile the platform displays. Anything else a provider sends is
// theirs and stays theirs.
type Identity struct {
	// Subject is the OIDC `sub` — stable across email changes at the provider,
	// which is why it and not the address is what the user row is keyed by.
	Subject string
	Email   string
	Name    string
}

// Verifier checks a bearer token against the configured OIDC provider.
//
// The provider is resolved lazily rather than at construction, so an identity
// provider that is briefly unreachable while the cluster comes up does not stop
// this service from starting — and a failed resolution is not cached, so it
// starts working when the provider does. Discovery is one request; go-oidc caches
// the key set behind the verifier it returns.
type Verifier struct {
	issuer    string
	audiences []string

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// NewVerifier returns a Verifier for the given issuer and accepted audiences.
//
// Audiences is a set rather than one value because one install presents more than
// one face to the same provider: the editor signs people in with a token minted
// for its client id, and an MCP client arrives with an access token minted for the
// `/mcp` resource identifier. Both are this platform, and both should be able to
// trade their token for a platform one.
//
// The check itself is ours rather than go-oidc's, which accepts a single client
// id. That is the reason it is spelled out below and tested in both directions:
// widening an audience check by accident is how a token minted for somebody
// else's application becomes a session here.
func NewVerifier(issuer string, audiences []string) *Verifier {
	return &Verifier{issuer: issuer, audiences: audiences}
}

// Verify checks rawToken and returns who it says the caller is. A token that
// fails any check — signature, issuer, audience, expiry — is ErrUnauthenticated,
// with the provider's reason wrapped for the log but not for the caller.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	provider, verifier, err := v.resolve(ctx)
	if err != nil {
		return Identity{}, err
	}

	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}

	if !v.audienceAccepted(token.Audience) {
		// The audiences are not named in the message. Which applications this
		// install accepts is not something an unauthenticated caller needs to learn
		// from a failed attempt.
		return Identity{}, fmt.Errorf(
			"%w: the token was minted for a different application", ErrUnauthenticated)
	}

	// Only what we use. A provider's token carries a great deal more, and none of
	// it is ours to store.
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: reading claims: %w", ErrUnauthenticated, err)
	}

	email, name := strings.TrimSpace(claims.Email), strings.TrimSpace(claims.Name)
	if email == "" {
		// An OAuth access token carries `sub` and little else — an MCP client's
		// bearer never has an email on it — so before refusing, ask the provider
		// who this is. An id token that simply was not asked for the email scope
		// lands here too, and the same call answers for it.
		email, name = v.fromUserinfo(ctx, provider, rawToken, name)
	}
	if email == "" {
		// Named specifically, because the fix is at the provider and not here: the
		// email scope has to be requested, and the claim has to reach either the
		// token or the userinfo endpoint.
		return Identity{}, fmt.Errorf(
			"%w: the token carries no email claim and the provider's userinfo did not "+
				"supply one; the provider must be configured to include an email for the "+
				"requested scopes", ErrUnauthenticated)
	}

	return Identity{Subject: token.Subject, Email: email, Name: name}, nil
}

// audienceAccepted reports whether any of the token's audiences is one this
// install answers for. A token carries a list, and matching any entry is the rule
// RFC 7519 lays down.
func (v *Verifier) audienceAccepted(tokenAudiences []string) bool {
	for _, aud := range tokenAudiences {
		if slices.Contains(v.audiences, aud) {
			return true
		}
	}
	return false
}

// fromUserinfo asks the provider for the caller's profile, using the caller's own
// token as the credential. It returns what it found, falling back to the name we
// already had.
//
// The credential is whatever was presented to us, which is right for the case
// this exists to serve: an MCP client arrives with an OAuth access token, and an
// access token is exactly what a userinfo endpoint wants. A sign-in arrives with
// an id token instead, and a conforming provider may well refuse that — which is
// acceptable, because an id token that carries no email at all is a provider
// misconfiguration, and the refusal below names it. What this must never do is
// turn either case into a different error about a lookup nobody asked for.
//
// Best-effort on purpose, for the same reason: a provider that publishes no
// userinfo endpoint, or one that is briefly unreachable, should produce the "no
// email" refusal the caller can act on.
func (v *Verifier) fromUserinfo(
	ctx context.Context, provider *oidc.Provider, rawToken, name string,
) (string, string) {
	info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: rawToken, TokenType: "Bearer"}))
	if err != nil {
		slog.DebugContext(ctx, "userinfo lookup failed while resolving an email", "error", err)
		return "", name
	}
	var claims struct {
		Name string `json:"name"`
	}
	if err := info.Claims(&claims); err == nil && strings.TrimSpace(claims.Name) != "" {
		name = strings.TrimSpace(claims.Name)
	}
	return strings.TrimSpace(info.Email), name
}

// resolve returns the verifier, performing discovery on the first call that needs
// it. A failure is returned and not remembered, so the next request tries again.
//
// The network call happens OUTSIDE the mutex, which matters when the provider is
// down. Holding the lock across it would make every concurrent sign-in queue
// behind one attempt and then start its own, so with a handful of callers most
// would burn their whole deadline waiting rather than being told promptly that
// the provider is unreachable. The lock guards only the field.
//
// The cost is that several first-time callers may each discover at once. That is
// one GET apiece, it happens only until one of them succeeds, and it is the
// cheaper of the two failure modes by a wide margin.
func (v *Verifier) resolve(ctx context.Context) (*oidc.Provider, *oidc.IDTokenVerifier, error) {
	if provider, verifier := v.cached(); verifier != nil {
		return provider, verifier, nil
	}

	provider, err := oidc.NewProvider(ctx, v.issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: discovering %s: %w", ErrProviderUnreachable, v.issuer, err)
	}
	// SkipClientIDCheck because the audience is checked in Verify against the whole
	// accepted set; go-oidc can only be told about one. Skipping it here does not
	// skip it — it moves it.
	verifier := provider.Verifier(&oidc.Config{SkipClientIDCheck: true})

	v.mu.Lock()
	defer v.mu.Unlock()
	// First writer wins. Two concurrent discoveries of the same issuer produce
	// equivalent verifiers, so which one is kept does not matter — only that every
	// later caller sees the same one and none of them discovers again.
	if v.verifier == nil {
		v.provider, v.verifier = provider, verifier
	}
	return v.provider, v.verifier, nil
}

// cached returns the resolved provider and verifier, or nils when discovery has
// not succeeded yet.
func (v *Verifier) cached() (*oidc.Provider, *oidc.IDTokenVerifier) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.provider, v.verifier
}
