package folder

import "errors"

var (
	// ErrNotFound is returned when a folder, or a referenced folder/integration
	// in a membership operation, does not exist.
	ErrNotFound = errors.New("folder not found")
	// ErrInvalid is returned when a folder fails validation (bad name, or a move
	// that would create a cycle in the tree).
	ErrInvalid = errors.New("folder invalid")
)
