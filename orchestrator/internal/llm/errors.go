package llm

import "errors"

var (
	// ErrInvalidProvider is returned when the provider is not one the runtime can
	// talk to.
	ErrInvalidProvider = errors.New("invalid provider")
	// ErrInvalidModel is returned when the model identifier is empty, too long, or
	// contains a line break.
	ErrInvalidModel = errors.New("invalid model")
	// ErrInvalidAPIKey is returned when a supplied key is too short to be real.
	ErrInvalidAPIKey = errors.New("invalid api key")
)
