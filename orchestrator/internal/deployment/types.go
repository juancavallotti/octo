// Package deployment is the orchestrator feature module for running an
// integration as its own Kubernetes workload: the domain model, its repository
// (the integration_deployments table), service-layer lifecycle logic, and HTTP
// handler. The actual cluster resources are managed through internal/kube.
package deployment

import (
	"encoding/json"
	"time"
)

// Deployment is one deployed instance of an integration. IDs are UUIDs in
// canonical text form. Settings carries user-supplied per-deployment config;
// Metadata carries orchestrator-owned bookkeeping. Both are stored as jsonb.
type Deployment struct {
	ID            string
	IntegrationID string
	Status        string
	Settings      json.RawMessage
	Metadata      json.RawMessage
	LastUpdated   time.Time
}

// Metadata is the orchestrator-owned bookkeeping stored in deployment_metadata.
// It is intentionally small for now and will grow (pod conditions, URLs, ...).
type Metadata struct {
	// Name is a human-facing label for the deployment, captured from the
	// integration's name at deploy time.
	Name string `json:"name,omitempty"`
}

// MetadataName extracts the display name from a deployment's metadata jsonb,
// returning "" when absent or unparseable.
func MetadataName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m Metadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	return m.Name
}
