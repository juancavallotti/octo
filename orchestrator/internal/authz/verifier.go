package authz

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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

	// ready is the fast path: once the keyset has been fetched, every request
	// reads it without touching the lock below.
	ready atomic.Pointer[oidc.IDTokenVerifier]

	mu sync.Mutex
	// retryAt is when a failed fetch may be tried again. Without it an iam that is
	// down turns every request into its own discovery attempt, each waiting out
	// its own timeout behind the lock, and the orchestrator serializes all of its
	// traffic on a dependency that is not answering.
	retryAt time.Time
}

// retryAfter is how long a failed keyset fetch is left alone. Short enough that
// an iam coming back is picked up promptly, long enough that a burst of requests
// costs one attempt rather than one each.
const retryAfter = 5 * time.Second

// discoveryTimeout caps one keyset fetch. The lock is held for its duration, so
// it is also the longest any request waits on a resolution somebody else
// started.
const discoveryTimeout = 5 * time.Second

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
		// NotBefore is read here because nothing else reads it: go-oidc exposes
		// `exp` and `iat` on the token and not `nbf`, and SkipExpiryCheck turns off
		// what time checking it does. Without this a token stamped to become valid
		// an hour from now is accepted the moment it is signed.
		NotBefore int64 `json:"nbf"`
	}
	if err := token.Claims(&claims); err != nil {
		return Principal{}, fmt.Errorf("%w: reading claims: %w", ErrUnauthenticated, err)
	}
	if claims.NotBefore != 0 &&
		time.Unix(claims.NotBefore, 0).Add(-clockSkew).After(now) {
		return Principal{}, fmt.Errorf("%w: the token is not valid yet", ErrUnauthenticated)
	}
	return Principal{
		Subject:    token.Subject,
		Roles:      claims.Roles,
		Deployment: claims.Deployment,
	}, nil
}

// resolve returns the verifier, fetching the keyset the first time.
//
// The common case takes no lock at all. The first request through does, and any
// arriving alongside it wait for that one fetch rather than starting their own —
// and if it fails they are turned away immediately until retryAfter has passed,
// so an iam that is down costs one attempt every few seconds instead of one per
// request.
func (v *Verifier) resolve(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	if ready := v.ready.Load(); ready != nil {
		return ready, nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	// Re-checked under the lock: another request may have fetched it while this
	// one waited.
	if ready := v.ready.Load(); ready != nil {
		return ready, nil
	}
	if time.Now().Before(v.retryAt) {
		return nil, fmt.Errorf("%w: the keyset could not be fetched", ErrUnavailable)
	}

	// Bounded, and detached from the caller. Without the deadline an iam that
	// accepts the connection and then says nothing holds this lock for as long as
	// the first request lives, and every other request queues behind it — which is
	// the serialization retryAt exists to prevent. Without the detachment a client
	// that simply hung up would be recorded as iam failing, and unrelated callers
	// would be turned away for it.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discoveryTimeout)
	defer cancel()

	// The client goes on the context because go-oidc keeps this context for the
	// life of the keyset, and uses it for every later fetch as well as this one.
	// Those later fetches are the ones that need it: when iam rotates its keys the
	// keyset re-fetches on the unknown kid, on a background context this deadline
	// does not reach, so without a client timeout a stalled iam blocks token
	// verification with no deadline at all.
	fetchCtx = oidc.ClientContext(fetchCtx, &http.Client{Timeout: discoveryTimeout})

	provider, err := oidc.NewProvider(fetchCtx, v.issuer)
	if err != nil {
		// Not cached as a permanent answer: iam may simply not be up yet, and a
		// failure remembered forever would mean this service never authorizes
		// anybody again until it restarts.
		v.retryAt = time.Now().Add(retryAfter)
		slog.WarnContext(ctx, "could not reach iam to fetch its keys", "issuer", v.issuer, "error", err)
		return nil, fmt.Errorf("%w: the keyset could not be fetched", ErrUnavailable)
	}
	ready := provider.Verifier(&oidc.Config{
		ClientID:             tokenAudience,
		SupportedSigningAlgs: []string{oidc.ES256},
		// Checked in Verify with a tolerance, against the same clock as `nbf`.
		SkipExpiryCheck: true,
	})
	v.ready.Store(ready)
	return ready, nil
}
