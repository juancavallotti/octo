package deployment

import "errors"

var (
	// ErrNotFound is returned when a deployment does not exist.
	ErrNotFound = errors.New("deployment not found")
	// ErrIntegrationNotFound is returned when the integration to deploy does not exist.
	ErrIntegrationNotFound = errors.New("integration not found")
	// ErrUnavailable is returned when Kubernetes access is not configured, so
	// deployments cannot be managed.
	ErrUnavailable = errors.New("deployments unavailable")
)
