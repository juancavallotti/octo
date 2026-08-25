package bundle

import "errors"

var (
	// ErrInvalid is returned when an archive is not a readable bundle: not a zip,
	// no definition, an unsafe entry name, or a manifest that does not parse.
	ErrInvalid = errors.New("bundle invalid")
	// ErrTooLarge is returned when an archive exceeds the size limits — too many
	// entries, or more uncompressed bytes than a bundle is allowed to expand to.
	ErrTooLarge = errors.New("bundle too large")
)
