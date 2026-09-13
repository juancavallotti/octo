package user

import "errors"

var (
	// ErrNotFound is returned when no user matches the given id or subject.
	ErrNotFound = errors.New("user not found")
	// ErrInvalid is returned when a request omits something required or names a role
	// outside the catalogue. Wrapped with the specific reason, which reaches the
	// caller: these are all mistakes a caller can correct.
	ErrInvalid = errors.New("invalid request")
	// ErrConflict is returned when a write would put two accounts on one address.
	// Distinct from ErrInvalid: the request was well-formed and the answer is that
	// the person is already here.
	ErrConflict = errors.New("that address already has an account")
	// ErrSubjectMismatch is returned when somebody authenticates with an address
	// that belongs to an account already claimed by a different principal at the
	// identity provider. Refused rather than resolved: an address decides who
	// somebody is exactly once, on the account's first sign-in, and after that the
	// subject is what the row is keyed by.
	ErrSubjectMismatch = errors.New("that address belongs to a different account")
	// ErrNotProvisioned is returned when somebody the identity provider vouches for
	// has no account here. Distinct from ErrNotFound, which is a caller naming a
	// user that does not exist: this is a caller correctly authenticated who has
	// not been let in.
	ErrNotProvisioned = errors.New("this account has not been provisioned on this platform")
	// ErrGranterGone is returned when a grant cannot be attributed because the
	// account making it no longer exists — deleted while its token was still valid.
	//
	// Separate from ErrNotFound because two foreign keys can refuse this write and
	// they name opposite users.
	ErrGranterGone = errors.New("the account making this grant no longer exists")
	// ErrLastAdmin is returned when an operation would leave the install with no
	// administrator — revoking the last admin role, or deleting the last person
	// holding it. With nobody able to administer it, the bootstrap hands the role to
	// the next stranger who signs in.
	ErrLastAdmin = errors.New("this is the last administrator")
)
