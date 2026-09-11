// Package caller carries the credential a request arrived with.
//
// One value, read from the Authorization header and put on the request context
// so a handler deep in a call chain can reach it without every function between
// taking a credential as an argument.
//
// It is not authentication. Nothing here checks a signature, and what it yields
// is "the bearer this caller presented", never "who this caller is". The one
// consumer spends it at iam, which verifies it and refuses it if it is not good.
package caller

import (
	"context"
	"net/http"
	"strings"
)

type contextKey struct{}

// Middleware records the bearer token, when the request carries one, for
// Token to read further down.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := bearer(r); token != "" {
			r = r.WithContext(With(r.Context(), token))
		}
		next.ServeHTTP(w, r)
	})
}

// With returns ctx carrying token, for a caller that has one in hand rather than
// in a header — a background reconcile acting on its own behalf, or a test.
func With(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, contextKey{}, token)
}

// Token returns the credential this request arrived with, or "" when it arrived
// with none. Every consumer has to handle the empty case anyway, because an
// install that is not enforcing sends no credential at all.
func Token(ctx context.Context) string {
	token, _ := ctx.Value(contextKey{}).(string)
	return token
}

// bearer reads the token out of the Authorization header. The scheme is
// case-insensitive per RFC 6750, and clients differ.
func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	const scheme = "bearer "
	if len(value) <= len(scheme) || !strings.EqualFold(value[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(value[len(scheme):])
}
