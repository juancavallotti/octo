package apikey

import "errors"

var (
	// ErrNotFound is returned when a key does not exist (or is already revoked).
	ErrNotFound = errors.New("api key not found")
	// ErrInvalidName is returned when a key's label is empty or too long.
	ErrInvalidName = errors.New("invalid api key name")
	// ErrInvalidTTL is returned when the requested expiration is non-positive or
	// beyond the allowed maximum.
	ErrInvalidTTL = errors.New("invalid api key expiration")
	// ErrUserNotFound is returned when creating a key for a user that does not
	// exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrExpired is returned when a presented token has passed its expiry.
	ErrExpired = errors.New("api key expired")
	// ErrInvalidToken is returned when a presented token matches no active key.
	ErrInvalidToken = errors.New("invalid api key")
)
