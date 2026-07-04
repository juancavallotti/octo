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
