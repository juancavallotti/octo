package websearch

import "errors"

// ErrInvalidAPIKey is returned when a supplied key is too short to be real, or so
// long it is not a key at all.
var ErrInvalidAPIKey = errors.New("invalid api key")
