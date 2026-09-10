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
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
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
	issuer   string
	clientID string

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewVerifier returns a Verifier for the given issuer and client id. The client
// id is the expected audience: a token minted for a different application at the
// same provider is not a token for us.
func NewVerifier(issuer, clientID string) *Verifier {
	return &Verifier{issuer: issuer, clientID: clientID}
}

// Verify checks rawToken and returns who it says the caller is. A token that
// fails any check — signature, issuer, audience, expiry — is ErrUnauthenticated,
// with the provider's reason wrapped for the log but not for the caller.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	verifier, err := v.resolve(ctx)
	if err != nil {
		return Identity{}, err
	}

	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
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

	email := strings.TrimSpace(claims.Email)
	if email == "" {
		// Named specifically, because the fix is at the provider and not here: the
		// email scope has to be requested and the claim has to be in the token
		// rather than only at the userinfo endpoint.
		return Identity{}, fmt.Errorf(
			"%w: the token carries no email claim; the provider must be configured to "+
				"include one for the requested scopes", ErrUnauthenticated)
	}

	return Identity{
		Subject: token.Subject,
		Email:   email,
		Name:    strings.TrimSpace(claims.Name),
	}, nil
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
func (v *Verifier) resolve(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	if cached := v.cached(); cached != nil {
		return cached, nil
	}

	provider, err := oidc.NewProvider(ctx, v.issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: discovering %s: %w", ErrProviderUnreachable, v.issuer, err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: v.clientID})

	v.mu.Lock()
	defer v.mu.Unlock()
	// First writer wins. Two concurrent discoveries of the same issuer produce
	// equivalent verifiers, so which one is kept does not matter — only that every
	// later caller sees the same one and none of them discovers again.
	if v.verifier == nil {
		v.verifier = verifier
	}
	return v.verifier, nil
}

// cached returns the resolved verifier, or nil when discovery has not succeeded
// yet.
func (v *Verifier) cached() *oidc.IDTokenVerifier {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.verifier
}
