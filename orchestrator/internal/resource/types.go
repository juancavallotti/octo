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
type Resource struct {
	ID            string
	IntegrationID string
	Kind          string
	Name          string
	Content       string
	CreatedAt     time.Time
	LastUpdated   time.Time
}
