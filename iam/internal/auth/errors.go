package auth

import "errors"

var (
	// ErrUnauthenticated is returned when the presented token is missing, or does
	// not verify against the configured provider. The reason is wrapped for the
	// log and deliberately not passed to the caller: "which check failed" is a
	// hint to whoever is guessing, and useless to whoever is not.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrProviderUnreachable is returned when the identity provider could not be
	// reached to discover its keys. It is a fault on our side of the exchange, not
	// the caller's, and is reported as one.
	ErrProviderUnreachable = errors.New("identity provider unreachable")
	// ErrNotConfigured is returned by NewService when no identity provider is
	// configured. The exchange then answers every request by saying so, rather
	// than the route disappearing — a 404 leaves a caller unable to tell a
	// misconfigured install from a version that never had the feature.
	ErrNotConfigured = errors.New("no identity provider is configured")
)
