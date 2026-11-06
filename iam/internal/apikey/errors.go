package apikey

import "errors"

var (
	// ErrNotFound is returned when a key does not exist (or is already revoked).
	ErrNotFound = errors.New("api key not found")
	// ErrInvalidName is returned when a key's label is empty or too long.
	ErrInvalidName = errors.New("invalid api key name")
	// ErrInvalidExpiry is returned when the requested expiration is not in the
	// future or is beyond the allowed maximum.
	ErrInvalidExpiry = errors.New("invalid api key expiration")
	// ErrInvalidRoles is returned when a key is asked for no roles, or for one a
	// key may not carry.
	ErrInvalidRoles = errors.New("invalid api key roles")
	// ErrEscalation is returned when a key is asked for a role its owner does not
	// reach.
	ErrEscalation = errors.New("an API key cannot carry a role its owner does not hold")
	// ErrUserNotFound is returned when creating a key for a user that does not
	// exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrExpired is returned when a presented token has passed its expiry.
	ErrExpired = errors.New("api key expired")
	// ErrUnknownEnvironment is returned when a key is asked to be bound to a name
	// that is not an environment.
	ErrUnknownEnvironment = errors.New("unknown environment")
	// ErrInvalidToken is returned when a presented token matches no active key.
	ErrInvalidToken = errors.New("invalid api key")
)
