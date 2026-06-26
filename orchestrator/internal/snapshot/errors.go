package snapshot

import "errors"

var (
	// ErrNotFound is returned when a snapshot does not exist.
	ErrNotFound = errors.New("snapshot not found")
	// ErrInvalid is returned when a tag fails validation (empty, too long, or
	// containing characters outside the allowed set).
	ErrInvalid = errors.New("snapshot invalid")
	// ErrTagExists is returned when a tag already exists for the integration.
	// Tags are immutable, so re-creating one is a conflict rather than an update.
	ErrTagExists = errors.New("snapshot tag already exists")
	// ErrIntegrationNotFound is returned when the integration to snapshot does not
	// exist.
	ErrIntegrationNotFound = errors.New("integration not found")
	// ErrSnapshotInUse is returned when a snapshot cannot be deleted because one or
	// more deployments still reference it. Deleting it would leave those
	// deployments pinned to a version that no longer exists.
	ErrSnapshotInUse = errors.New("snapshot is currently deployed")
)
