package sitesettings

import "errors"

var (
	// ErrEncryptionUnavailable is returned when a caller supplies an API key but no
	// cipher is configured. Storing the key in the clear instead is not an option
	// the caller gets: a rejected save is recoverable, a plaintext secret in the
	// database is not.
	ErrEncryptionUnavailable = errors.New("encryption is not configured")
	// ErrNotConfigured is returned when an operation needs a stored API key and
	// there is none.
	ErrNotConfigured = errors.New("not configured")
)
