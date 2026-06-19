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

// Settings is the user-supplied per-deployment config stored in the settings
// jsonb. Fields are optional; zero values mean "use the default".
type Settings struct {
	// Replicas is the desired runtime replica count; <1 is normalized to 1. The
	// per-deployment Service load-balances across them for internal callers.
	Replicas int `json:"replicas,omitempty"`
	// Expose opts the deployment into an external HTTP endpoint. "external"
	// publishes a {subdomain}.{baseDomain} Ingress with TLS; empty = internal only.
	Expose string `json:"expose,omitempty"`
	// Subdomain is the external host label; empty defaults to the integration
	// slug. Only meaningful when Expose is "external".
	Subdomain string `json:"subdomain,omitempty"`
}

// ExposeExternal is the Settings.Expose value that requests a public endpoint.
const ExposeExternal = "external"

// External reports whether these settings request an external endpoint.
func (s Settings) External() bool { return s.Expose == ExposeExternal }

// Metadata is the orchestrator-owned bookkeeping stored in deployment_metadata.
type Metadata struct {
	// Name is a human-facing label for the deployment, captured from the
	// integration's name at deploy time.
	Name string `json:"name,omitempty"`
	// Slug is the DNS-1123 slug of the integration name; it names the stable
	// internal Service (octo-int-{slug}). Empty when the name has no usable slug.
	Slug string `json:"slug,omitempty"`
	// InternalURL is the in-cluster address other flows use to reach this
	// integration, load-balanced across replicas (and across deployments of the
	// same integration). Empty when there is no slug.
	InternalURL string `json:"internalUrl,omitempty"`
	// ExternalURL is the public https://{subdomain}.{baseDomain} address when the
	// deployment is exposed externally; empty for internal-only deployments.
	ExternalURL string `json:"externalUrl,omitempty"`
}

// ParseMetadata unmarshals the metadata jsonb, returning a zero Metadata when
// absent or unparseable.
func ParseMetadata(raw json.RawMessage) Metadata {
	var m Metadata
	if len(raw) == 0 {
		return m
	}
	_ = json.Unmarshal(raw, &m)
	return m
}

// MetadataName extracts the display name from a deployment's metadata jsonb,
// returning "" when absent or unparseable.
func MetadataName(raw json.RawMessage) string {
	return ParseMetadata(raw).Name
}

// ParseSettings unmarshals the settings jsonb, returning a zero Settings when
// absent or unparseable.
func ParseSettings(raw json.RawMessage) Settings {
	var s Settings
	if len(raw) == 0 {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}
