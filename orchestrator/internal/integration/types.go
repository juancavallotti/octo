// Package integration is the orchestrator feature module for authored
// integrations: the domain model, its repository, service-layer business logic
// and HTTP handler. The repository persists to the integrations table; folders
// and deployments are modelled in the schema but not yet exposed here.
package integration

import "time"

// Integration is the stored definition of an integration. IDs are UUIDs in
// canonical text form; pgx's UUID codec scans them to and from Go strings.
//
// CreatedBy/UpdatedBy are the user ids that authored and last edited the
// integration; the *Email/*Name fields are those users resolved for display via
// a join on reads. All are pointers because they are nullable — a row written
// without a known actor (e.g. via the MCP path, or local dev without SSO) has no
// attribution, and a referenced user may since have been removed.
type Integration struct {
	ID          string
	Name        string
	Definition  string
	LastUpdated time.Time

	CreatedBy      *string
	UpdatedBy      *string
	CreatedByEmail *string
	CreatedByName  *string
	UpdatedByEmail *string
	UpdatedByName  *string
}
