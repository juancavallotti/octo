package signing

import "errors"

var (
	// ErrNoKey is returned when the set holds no key that can sign — which, since
	// the service mints one on demand, means only that it could not.
	ErrNoKey = errors.New("no signing key is available")
	// ErrInvalidConfig is returned by NewService when the settings it is given
	// cannot produce a coherent keyset.
	ErrInvalidConfig = errors.New("invalid signing configuration")
)
