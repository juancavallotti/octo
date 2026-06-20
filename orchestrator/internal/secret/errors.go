package secret

import "errors"

var (
	// ErrInvalidName is returned when a secret name is not a valid env-var/Secret
	// key (uppercase identifier).
	ErrInvalidName = errors.New("invalid secret name")
	// ErrNotFound is returned when a secret does not exist in the catalog.
	ErrNotFound = errors.New("secret not found")
	// ErrUnavailable is returned when Kubernetes access is not configured, so
	// secret values cannot be stored.
	ErrUnavailable = errors.New("secrets unavailable")
	// ErrInUse is returned when deleting a secret still referenced by a deployment
	// (override with force).
	ErrInUse = errors.New("secret in use by a deployment")
)
