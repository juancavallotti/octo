package authz

import "errors"

var (
	// ErrUnauthenticated is a caller this service cannot identify: no token, or
	// one it will not accept. It answers 401.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden is a caller it identified and will not serve. It answers 403.
	ErrForbidden = errors.New("forbidden")
	// ErrUnavailable is this service being unable to decide, which is neither of
	// the above and must not be reported as either: a caller told "forbidden"
	// because iam was down will go and change permissions that were never wrong.
	ErrUnavailable = errors.New("authorization is unavailable")
)
