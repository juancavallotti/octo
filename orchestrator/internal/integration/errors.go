package integration

import (
	"errors"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

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
	// identity, or a file that is not a YAML mapping. It is the merge's own error,
	// so the merge's message, which already names the conflict, is the whole of it.
	ErrFilesConflict = projectfile.ErrMerge
)
