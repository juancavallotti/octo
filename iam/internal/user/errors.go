package user

import "errors"

var (
	// ErrNotFound is returned when no user matches the given id or subject.
	ErrNotFound = errors.New("user not found")
	// ErrInvalid is returned when a request omits something required or names a
	// role outside the catalogue. Wrapped with the specific reason, which the
	// handler passes through to the caller — these are all mistakes a caller can
	// correct, so saying which one it was is worth more than uniformity.
	ErrInvalid = errors.New("invalid request")
	// ErrConflict is returned when creating a user whose OIDC subject already has
	// an account. Distinct from ErrInvalid because the caller's request was
	// well-formed and the answer is "that person is already here", which is a
	// different thing to tell somebody.
	ErrConflict = errors.New("that subject already has an account")
	// ErrNotProvisioned is returned when somebody the identity provider vouches
	// for has no account here. Distinct from ErrNotFound, which is a caller naming
	// a user that does not exist: this is a real person, correctly authenticated,
	// who has simply not been let in — and the two want different words said to
	// them.
	ErrNotProvisioned = errors.New("this account has not been provisioned on this platform")
	// ErrLastAdmin is returned when an operation would leave the platform with no
	// administrator — revoking the last admin role, or deleting the last person
	// holding it. Not merely inconvenient: with nobody able to administer it, the
	// bootstrap hands the role to the next stranger who signs in.
	ErrLastAdmin = errors.New("this is the last administrator")
)
