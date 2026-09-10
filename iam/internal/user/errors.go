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
)
