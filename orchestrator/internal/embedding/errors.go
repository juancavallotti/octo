package embedding

import "errors"

var (
	// ErrInvalidProvider is returned when the provider has no embeddings endpoint.
	ErrInvalidProvider = errors.New("invalid embedding provider")
	// ErrInvalidModel is returned when the model identifier is empty, too long, or
	// contains a line break.
	ErrInvalidModel = errors.New("invalid embedding model")
	// ErrInvalidAPIKey is returned when a supplied key is too short to be real.
	ErrInvalidAPIKey = errors.New("invalid api key")
	// ErrNotConfigured is returned by the embedder when nothing has been set up. It
	// is not a failure: it is what a deployment that has not turned this on looks
	// like, and search falls back to full text.
	ErrNotConfigured = errors.New("embeddings are not configured")
	// ErrWrongDimensions is returned when the provider answers with vectors of a
	// width the column cannot hold. It is a configuration mistake — the wrong model
	// — and it is caught rather than stored, because a table of mixed widths does
	// not fail at write time, it fails at query time with nothing pointing at why.
	ErrWrongDimensions = errors.New("the model does not produce vectors of the stored width")
)
