package kv

import "errors"

var (
	// ErrVersionConflict is returned by a write whose expected version does not
	// match the stored row, so the caller refreshes and retries. The handler maps
	// it to 409 Conflict.
	ErrVersionConflict = errors.New("kv version conflict")
	// ErrEncryptionDisabled is returned by a secret write when no encryption key is
	// configured. It lives here (not in the secret store) so the shared handler can
	// map it to 503 without importing the secret package.
	ErrEncryptionDisabled = errors.New("kv encryption disabled")
)
