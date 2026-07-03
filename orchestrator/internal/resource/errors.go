package resource

import "errors"

var (
	// ErrNotFound is returned when a resource does not exist (or does not belong
	// to the integration it was addressed under).
	ErrNotFound = errors.New("resource not found")
	// ErrInvalid is returned when a resource fails validation (bad kind or name).
	ErrInvalid = errors.New("resource invalid")
	// ErrIntegrationNotFound is returned when the owning integration does not exist.
	ErrIntegrationNotFound = errors.New("integration not found")
	// ErrNameExists is returned when a resource with the same name already exists
	// for the integration; a name is unique per integration.
	ErrNameExists = errors.New("resource name already exists")
)
