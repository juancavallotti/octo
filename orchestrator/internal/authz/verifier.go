package authz

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Verifying a platform token against the keyset that signed it.
//
// The keys come from iam's JWKS, fetched through discovery and cached by the
// library, which re-fetches when it meets a key id it has not seen — so a
// rotation needs nothing here.

// tokenAudience is the `aud` every platform token carries.
const tokenAudience = "octo"

// clockSkew is the tolerance on the two time claims. Tokens are stamped by one
// machine and checked by another, and a request refused for arriving a second
// too early is the least diagnosable failure there is.
const clockSkew = 60 * time.Second

// Verifier checks bearer tokens against iam's published keys.
//
// The provider is resolved lazily rather than at construction, so an iam that is
// briefly unreachable while the cluster comes up does not stop this service from
// starting; a failed resolution is not cached, so it starts working when iam
// does.
type Verifier struct {
	issuer string

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewVerifier returns a Verifier for the iam at issuer.
func NewVerifier(issuer string) *Verifier {
	return &Verifier{issuer: strings.TrimRight(strings.TrimSpace(issuer), "/")}
}

// Principal is who a verified token says its bearer is.
type Principal struct {
	// Subject is the platform user id. For a deployment's own token it is the
	// user the deployment was minted on behalf of, not the deployment.
	Subject string
	// Roles is what the token carries, and what the policy is applied to.
	Roles []string
	// Deployment names the deployment a machine token belongs to, and is empty
	// for a person's.
	Deployment string
}

// Has reports whether this principal holds any of roles.
func (p Principal) Has(roles ...string) bool {
	for _, want := range roles {
		for _, held := range p.Roles {
			if held == want {
				return true
			}
		}
	}
	return false
}

// Verify checks raw and returns who it says the caller is.
func (v *Verifier) Verify(ctx context.Context, raw string) (Principal, error) {
	verifier, err := v.resolve(ctx)
	if err != nil {
		return Principal{}, err
	}

	token, err := verifier.Verify(ctx, raw)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}

	// go-oidc checks expiry against its own clock with no tolerance, and iam
	// stamps `nbf` a little in the past for exactly this reason. Both horizons are
	// re-checked here with a skew allowance, so two machines a few seconds apart
	// do not produce a failure nobody can reproduce.
	now := time.Now()
	if token.Expiry.Add(clockSkew).Before(now) {
		return Principal{}, fmt.Errorf("%w: the token expired at %s",
			ErrUnauthenticated, token.Expiry.UTC())
	}
	if token.IssuedAt.Add(-clockSkew).After(now) {
		return Principal{}, fmt.Errorf("%w: the token was issued in the future", ErrUnauthenticated)
	}

	var claims struct {
		Roles      []string `json:"roles"`
		Deployment string   `json:"deployment"`
	}
	if err := token.Claims(&claims); err != nil {
		return Principal{}, fmt.Errorf("%w: reading claims: %w", ErrUnauthenticated, err)
	}
	return Principal{
		Subject:    token.Subject,
		Roles:      claims.Roles,
		Deployment: claims.Deployment,
	}, nil
}

// resolve builds the verifier on first use, outside the lock where it can.
func (v *Verifier) resolve(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}

	provider, err := oidc.NewProvider(ctx, v.issuer)
	if err != nil {
		// Not cached: iam may simply not be up yet, and a permanent failure here
		// would mean the orchestrator never authorizes anybody until it restarts.
		slog.WarnContext(ctx, "could not reach iam to fetch its keys", "issuer", v.issuer, "error", err)
		return nil, fmt.Errorf("%w: the keyset could not be fetched", ErrUnavailable)
	}
	v.verifier = provider.Verifier(&oidc.Config{
		ClientID:             tokenAudience,
		SupportedSigningAlgs: []string{oidc.ES256},
		// Checked below with a tolerance, against the same clock as `nbf`.
		SkipExpiryCheck: true,
	})
	return v.verifier, nil
}
