// Package authz decides whether a request may be served.
//
// It answers three questions in order: is this caller anybody, what does this
// route require, and does the caller hold it. What a route requires is in
// policy.go; who the caller is comes from a token iam signed.
//
// The raw credential stays where internal/caller put it, so a handler that has
// to act on the caller's behalf further down can still reach it. This adds the
// verified principal beside it rather than replacing it.
package authz

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/juancavallotti/octo/orchestrator/internal/caller"
	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

type contextKey struct{}

// checker is the verification this middleware needs, declared here so a test can
// substitute one without a keyset. *Verifier satisfies it.
type checker interface {
	Verify(ctx context.Context, raw string) (Principal, error)
}

// Wrap returns next guarded by the policy.
//
// Every request is refused unless a rule admits it, including requests for
// routes nobody wrote a rule for — see required.
func Wrap(v checker, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token := caller.Token(r.Context())
		if token == "" {
			// The challenge says what this endpoint wants, which is the one useful
			// thing to tell a caller who has not authenticated.
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, http.StatusUnauthorized, "a bearer token is required")
			return
		}

		principal, err := v.Verify(r.Context(), token)
		if err != nil {
			if errors.Is(err, ErrUnavailable) {
				// Deliberately not 403. A caller told they are forbidden will go and
				// change permissions that were never the problem.
				httpx.WriteError(w, http.StatusServiceUnavailable,
					"this request cannot be authorized right now")
				return
			}
			slog.DebugContext(r.Context(), "refused a token", "path", r.URL.Path, "error", err)
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, http.StatusUnauthorized, "the token is not valid")
			return
		}

		// A machine token may only act on its own deployment, whatever its roles
		// admit. Refused as a plain 403 rather than something more specific: a pod
		// reaching for another deployment is either a bug or a stolen credential,
		// and neither is owed the distinction.
		if !ownsTarget(principal, r.URL.Path) {
			slog.WarnContext(r.Context(), "a deployment reached for another one",
				"deployment", principal.Deployment, "path", r.URL.Path)
			httpx.WriteError(w, http.StatusForbidden,
				"this account may not perform that operation")
			return
		}

		allowed := required(r.Method, r.URL.Path)
		if len(allowed) == 0 || !principal.Has(allowed...) {
			// The roles are not named. What this install requires is not something an
			// unauthorized caller should learn by asking.
			httpx.WriteError(w, http.StatusForbidden,
				"this account may not perform that operation")
			return
		}

		next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), principal)))
	})
}

// NewContext carries a verified caller on ctx. Wrap is what calls it in
// production; it is exported because a handler that has to reason about the
// caller is otherwise untestable without standing up a keyset.
func NewContext(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, principal)
}

// FromContext returns the verified caller, and whether there was one. There is
// none on a request that reached an exempt route, and none at all when
// enforcement is off.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}
