// Package deployment is the orchestrator feature module for running an
// integration as its own Kubernetes workload: the domain model, its repository
// (the integration_deployments table), service-layer lifecycle logic, and HTTP
// handler. The actual cluster resources are managed through internal/kube.
package deployment

import (
	"encoding/json"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
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
	// Detail is the live cluster status populated on read (replicas, pods,
	// failure reason). Not persisted; the coarse Status string is the cached value.
	Detail kube.Status
}

// Settings is the user-supplied per-deployment config stored in the settings
// jsonb. Fields are optional; zero values mean "use the default".
type Settings struct {
	// Replicas is the desired runtime replica count; <1 is normalized to 1. The
	// per-deployment Service load-balances across them for internal callers.
	Replicas int `json:"replicas,omitempty"`
	// Slug is the user-chosen internal address label for a networked deployment
	// (the internal Service is octo-int-{slug}). It must be unique across
	// deployments; empty asks the orchestrator to allocate a free one. Ignored for
	// integrations with no HTTP source. Input only — the resolved slug lives in
	// Metadata.
	Slug string `json:"slug,omitempty"`
	// Expose opts the deployment into an external HTTP endpoint. "external"
	// publishes a {subdomain}.{baseDomain} Ingress with TLS; empty = internal only.
	Expose string `json:"expose,omitempty"`
	// Subdomain is the external host label; empty defaults to the integration
	// slug. Only meaningful when Expose is "external".
	Subdomain string `json:"subdomain,omitempty"`
	// Env binds the integration's declared environment variables for this
	// deployment, keyed by env var name. Each binding is either a literal value or a
	// reference to a cluster secret. HTTP_PORT/HTTP_HOST are orchestrator-managed and
	// cannot be bound here. Literal values are persisted as-is; secret bindings
	// persist only the secret name, never its value.
	Env map[string]EnvBinding `json:"env,omitempty"`
}

// EnvBinding is how one declared environment variable is filled at deploy time:
// either a literal Value or a reference to a cluster Secret (by name). A binding is
// a secret reference iff Secret is non-empty (Secret then wins over Value).
type EnvBinding struct {
	Value  string `json:"value,omitempty"`
	Secret string `json:"secret,omitempty"`
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
	// Slug is the DNS-1123 slug naming this deployment's internal Service
	// (octo-int-{slug}). It is unique across deployments — derived from the
	// integration name with a -NNN suffix on collision. Empty for deployments with
	// no HTTP source (no Service is created for those).
	Slug string `json:"slug,omitempty"`
	// InternalURL is the in-cluster address other flows use to reach this
	// deployment, load-balanced across its replicas. Empty when there is no slug
	// (the integration declares no HTTP source).
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
