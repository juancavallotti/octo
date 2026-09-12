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
	// ErrUnavailable is returned when this service could not answer — the keyset
	// could not be read, or the user could not be looked up — as opposed to the
	// token failing to verify.
	//
	// The distinction is the whole of it, and it is worth stating why. A platform
	// told "unauthenticated" clears the session and sends the person back to sign
	// in; a platform told "unavailable" keeps the credential it has and carries
	// on. So reporting a database blip as a bad token signs out every session
	// that happened to be inside its renewal window, which is a moment's trouble
	// at the database turning into an installation-wide sign-out.
	ErrUnavailable = errors.New("this service cannot answer right now")
	// ErrNotConfigured is returned by NewService when no identity provider is
	// configured. The exchange then answers every request by saying so, rather
	// than the route disappearing — a 404 leaves a caller unable to tell a
	// misconfigured install from a version that never had the feature.
	ErrNotConfigured = errors.New("no identity provider is configured")
	// ErrForbidden is returned when the caller is exactly who they say they are
	// and still may not have what they asked for. Distinct from
	// ErrUnauthenticated because the answer is different: there is nothing to
	// retry, and no credential that would help.
	ErrForbidden = errors.New("forbidden")
)
