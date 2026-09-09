// Package resource is the orchestrator feature module for an integration's
// resources: the env files and text templates the runtime loads at deploy time.
// It holds the domain model, its repository, service-layer validation and HTTP
// handler. Live resources persist to integration_resources; a tag freezes a copy
// into integration_resource_snapshots (see the snapshot package), which a deployed
// runtime reads back through the frozen-resource endpoints here.
package resource

import "time"

// Kind identifies what a resource is. The values are the wire/DB contract and
// mirror the runtime's core.ResourceKind, kept as plain strings here so the
// orchestrator does not depend on the runtime module.
const (
	KindEnv      = "env"
	KindTemplate = "template"
)

// Resource is a stored integration resource. IDs are UUIDs in canonical text
// form; pgx's UUID codec scans them to and from Go strings. Name is the resource
// id the config references and is path-like (it may contain '/').
//
// CreatedBy/UpdatedBy are the user ids that authored and last edited the file;
// the *Email/*Name fields are those users resolved for display via a join on
// reads. All are pointers because they are nullable — a row written without a
// known actor (the MCP path, or local dev without SSO) has no attribution, and a
// referenced user may since have been removed. Same shape, and the same reasons,
// as the integration row.
type Resource struct {
	ID            string
	IntegrationID string
	Kind          string
	Name          string
	Content       string
	CreatedAt     time.Time
	LastUpdated   time.Time

	CreatedBy      *string
	UpdatedBy      *string
	CreatedByEmail *string
	CreatedByName  *string
	UpdatedByEmail *string
	UpdatedByName  *string
}
