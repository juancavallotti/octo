package integration

import "errors"

var (
	// ErrNotFound is returned when an integration does not exist.
	ErrNotFound = errors.New("integration not found")
	// ErrInvalid is returned when an integration fails validation.
	ErrInvalid = errors.New("integration invalid")
	// ErrNameTaken is returned when another integration already uses the name
	// (compared case-insensitively).
	ErrNameTaken = errors.New("integration name already in use")
)

// ErrAmbiguousDefinition is returned when a whole-definition write lands on an
// integration that has more than one config file. The caller has no way to say
// which file it means, and guessing would collapse the project into one file,
// so the write is refused. Only a bundle import can produce that state today.
var ErrAmbiguousDefinition = errors.New("integration has several config files, so a whole-definition write is ambiguous")
