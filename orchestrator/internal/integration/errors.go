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
	// ErrFileNotFound is returned when an integration has no config file at the
	// path asked for.
	ErrFileNotFound = errors.New("config file not found")
	// ErrFilesConflict is returned when a write would leave config files that do
	// not merge into one config: a name claimed in two files, a second service
	// identity, or a file that is not a YAML mapping.
	ErrFilesConflict = errors.New("config files conflict")
)

// ErrAmbiguousDefinition is returned when a whole-definition write lands on an
// integration that has more than one config file. The caller has no way to say
// which file it means, and guessing would collapse the project into one file,
// so the write is refused. Only a bundle import can produce that state today.
var ErrAmbiguousDefinition = errors.New("integration has several config files, so a whole-definition write is ambiguous")
