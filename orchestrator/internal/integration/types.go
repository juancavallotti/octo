// Package integration is the orchestrator feature module for authored
// integrations: the domain model, its repository, service-layer business logic
// and HTTP handler. The repository persists to the integrations table; folders
// and deployments are modelled in the schema but not yet exposed here.
package integration

import "time"

// Integration is the stored definition of an integration. IDs are UUIDs in
// canonical text form; pgx's UUID codec scans them to and from Go strings.
type Integration struct {
	ID          string
	Name        string
	Definition  string
	LastUpdated time.Time
}
