// Package current carries the environment a request acts in.
//
// A request names it with the X-Octo-Environment header. An absent header, or
// "octo", is the default environment, which this package spells as the empty
// string throughout — the same spelling tokens and environment-scoped columns
// use — so there is exactly one way to say "default" past the edge.
//
// Three steps, in this order on every request:
//
//   - Parse reads the header onto the context, before authorization, so the
//     guard can judge the caller by their roles in that environment.
//   - The guard (internal/authz) may fill it in: a token bound to one
//     environment that did not name one acts in its own.
//   - Require refuses a name that is not an environment, after authorization,
//     so a caller who has not authenticated cannot use it to learn which
//     environments exist.
package current

import (
	"context"
	"net/http"
	"strings"
)

// Header is where a request names its environment.
const Header = "X-Octo-Environment"

// Default is the default environment's name, accepted in the header and
// normalized to "".
const Default = "octo"

type contextKey struct{}

type selection struct {
	name     string
	explicit bool
}

// With carries environment on ctx. explicit records that the request named it,
// which is what lets a bound token without a header fall back to its own.
func With(ctx context.Context, environment string, explicit bool) context.Context {
	return context.WithValue(ctx, contextKey{}, selection{name: Normalize(environment), explicit: explicit})
}

// From returns the environment the request acts in, "" for the default.
func From(ctx context.Context) string {
	s, _ := ctx.Value(contextKey{}).(selection)
	return s.name
}

// Explicit reports whether the request named its environment.
func Explicit(ctx context.Context) bool {
	s, _ := ctx.Value(contextKey{}).(selection)
	return s.explicit
}

// Column returns the environment as a nullable column value: nil for the
// default, which every environment-scoped table stores as NULL.
func Column(ctx context.Context) *string {
	name := From(ctx)
	if name == "" {
		return nil
	}
	return &name
}

// Normalize maps the default environment's name to "" and trims whitespace.
func Normalize(environment string) string {
	environment = strings.TrimSpace(environment)
	if environment == Default {
		return ""
	}
	return environment
}

// DisplayName names an environment for people and the wire: the default's name
// for "".
func DisplayName(environment string) string {
	if environment == "" {
		return Default
	}
	return environment
}

// Parse reads the header onto the request's context. It refuses nothing; see
// Require.
func Parse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(Header)
		ctx := With(r.Context(), raw, strings.TrimSpace(raw) != "")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
