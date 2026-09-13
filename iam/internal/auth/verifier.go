// Package auth is the token exchange: it takes the bearer token an identity
// provider issued at sign-in, verifies it, resolves the caller to a platform user
// with roles, and mints an internal token signed by this service.
//
// It is the one place that speaks OIDC: what the provider says is converted into
// a token verifiable against the keys iam publishes, which takes no knowledge of
// OIDC to check.
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

// Identity is what an identity provider told us about the caller: the subject
// that keys the user row and the two pieces of profile kept with it. Anything else
// a provider sends is not read.
type Identity struct {
	// Subject is the OIDC `sub` — stable across email changes at the provider,
	// which is why it and not the address is what the user row is keyed by once
	// it is known.
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
// Audiences is a set rather than one value because one install can present more
// than one audience to the same provider, and a token minted for any of them may
// be traded for a platform token.
//
// The check is spelled out in audienceAccepted rather than left to go-oidc, which
// accepts a single client id.
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
		// The accepted audiences are not named: an unauthenticated caller does not
		// learn which applications this install answers for.
		return Identity{}, fmt.Errorf(
			"%w: the token was minted for a different application", ErrUnauthenticated)
	}

	// Only what is used. A provider's token carries more, and none of it is stored.
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: reading claims: %w", ErrUnauthenticated, err)
	}

	email, name := strings.TrimSpace(claims.Email), strings.TrimSpace(claims.Name)
	if email == "" {
		// An OAuth access token carries `sub` and little else, and an id token minted
		// without the email scope lands here too, so ask the provider before
		// refusing.
		email, name = v.fromUserinfo(ctx, provider, rawToken, name)
	}
	if email == "" {
		// Named specifically, because the fix is at the provider: the email scope has
		// to be requested and the claim has to reach the token or userinfo.
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
// The credential is whatever was presented: an access token is what a userinfo
// endpoint wants, and a provider may refuse an id token there. Either way this is
// best-effort — a provider with no userinfo endpoint, or one briefly unreachable,
// leaves the caller with the "no email" refusal rather than an error about a
// lookup nobody asked for.
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
// The network call happens OUTSIDE the mutex, so a provider that is down does not
// make every concurrent sign-in queue behind one attempt and then start its own;
// the lock guards only the field. The cost is one extra GET per first-time caller
// until one of them succeeds.
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
	// First writer wins: two concurrent discoveries of the same issuer produce
	// equivalent verifiers, so only sharing one of them matters.
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
