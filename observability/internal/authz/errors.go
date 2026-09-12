package authz

import "errors"

var (
	// ErrUnauthenticated is a caller this service cannot identify: no token, or
	// one it will not accept. It answers 401.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrUnavailable is this service being unable to decide, which must not be
	// reported as a refusal: a caller told "forbidden" because the keyset could
	// not be fetched will go and change permissions that were never wrong.
	ErrUnavailable = errors.New("authorization is unavailable")
)
