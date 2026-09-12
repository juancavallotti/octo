// Package authz decides whether a caller may reach iam's own management routes.
//
// It is the answer to the note that stood in the user handler for as long as
// those routes existed: role grants were recorded with no grantor, because there
// was nobody behind them to record. There is now.
//
// The token it checks is one this service minted. That is the whole shape of it
// — iam signs in the platform's callers and then holds them to the same
// credential everything else does, rather than inventing a second kind of
// admission for itself.
package authz

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-jose/go-jose/v4/jwt"
	httpx "github.com/juancavallotti/octo/iam/internal/http"
)

// verifier is the one thing this needs from the keyset, declared here in the
// consumer so the guard can be tested without one. *signing.Service satisfies it.
type verifier interface {
	Verify(ctx context.Context, raw string, private any) (jwt.Claims, error)
}

// Principal is who a verified token speaks for.
//
// Roles are plain strings rather than user.Role, which keeps this package out of
// the user package's way: user imports this one to read the caller off a request,
// and the other direction would be a cycle.
type Principal struct {
	Subject string
	Roles   []string
}

// HasRole reports whether the principal holds role.
func (p Principal) HasRole(role string) bool { return slices.Contains(p.Roles, role) }

type contextKey struct{}

// principalClaims is the half of a platform token this package reads. It is the
// same `roles` the exchange stamps; only the field it needs is named.
type principalClaims struct {
	Roles []string `json:"roles"`
}

const (
	authHeader   = "Authorization"
	bearerPrefix = "Bearer "
)

// Require wraps next so that only a caller presenting a valid platform token
// holding every role in `roles` reaches it.
//
// No grace window is allowed: an expired token is a credential for renewing
// itself at POST /auth/refresh and for nothing else. Widening that here would
// make every management route accept a token ten minutes after it died.
func Require(v verifier, roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				w.Header().Set(`WWW-Authenticate`, `Bearer`)
				httpx.WriteError(w, http.StatusUnauthorized, "a bearer token is required")
				return
			}

			var private principalClaims
			claims, err := v.Verify(r.Context(), token, &private)
			if err != nil {
				// Logged in full, answered in one word — the reason a token failed is
				// a hint to whoever is guessing at one.
				slog.Info("iam refused a token on a management route", "error", err)
				w.Header().Set(`WWW-Authenticate`, `Bearer error="invalid_token"`)
				httpx.WriteError(w, http.StatusUnauthorized, "the token is not valid")
				return
			}

			p := Principal{Subject: claims.Subject, Roles: private.Roles}
			for _, want := range roles {
				if !p.HasRole(want) {
					// The role is named. A caller who holds the wrong one can act on
					// that, and it tells them nothing they could not learn by reading
					// the documentation.
					httpx.WriteError(w, http.StatusForbidden, "this requires the "+want+" role")
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), p)))
		})
	}
}

// NewContext returns ctx carrying p as the authenticated caller.
//
// Require does this itself; it is exported for the callers that establish a
// principal some other way — a test standing in for a guard, and whatever second
// kind of admission this service eventually grows.
func NewContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// ErrNoPrincipal is returned by FromContext when no guard ran. It is a
// programming error rather than a caller's, so a handler that meets it should
// fail rather than carry on as somebody.
var ErrNoPrincipal = errors.New("no authenticated principal on this request")

// FromContext returns the caller a guard put on the request.
func FromContext(ctx context.Context) (Principal, error) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	if !ok {
		return Principal{}, ErrNoPrincipal
	}
	return p, nil
}

// bearerToken reads the presented credential out of the Authorization header,
// returning "" when there is none to read. The prefix match is
// case-insensitive, per RFC 6750.
func bearerToken(r *http.Request) string {
	value := r.Header.Get(authHeader)
	if len(value) < len(bearerPrefix) ||
		!strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(value[len(bearerPrefix):])
}
