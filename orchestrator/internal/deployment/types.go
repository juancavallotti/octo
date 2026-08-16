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
	// Tracing runs this deployment's pods with the runtime's tracer on, so every
	// flow, block and model call they execute is published for the platform to
	// store. It is off by default and costs throughput, so it is a per-deployment
	// switch rather than an integration-wide one: you turn it on for the deployment
	// you are investigating.
	Tracing bool `json:"tracing,omitempty"`
	// OrchestratorAPI declares that this deployment's flows call the orchestrator's
	// own API — the platform agent being the first, but any integration that reads
	// its own installation is another.
	//
	// It grants nothing today: ORCHESTRATOR_URL is already in every runtime pod,
	// because the k8s services module needs it for the KV store and leader election,
	// and taking it away would break both. What this records is the *intent*, which
	// is what a future access model gates on — a deployment that never declared it
	// has no business calling the API, and saying so now means the enforcement point
	// arrives with the declarations already in place rather than needing every
	// existing deployment reclassified.
	OrchestratorAPI bool `json:"orchestratorApi,omitempty"`
	// ObservabilityAPI grants this deployment's flows the address of the log
	// aggregator's query API, injected as LOGS_URL. Unlike the orchestrator's, that
	// address is in no pod otherwise, so this switch is the whole of the access.
	//
	// Off by default: stored logs and traces span every deployment on the install,
	// so an integration that can read them can read its neighbours' — which is a
	// thing to ask for rather than to receive by default.
	ObservabilityAPI bool `json:"observabilityApi,omitempty"`
	// Runner selects the image this deployment's pods run. Empty and "standard"
	// are the generic octo-runtime: distroless, one static binary, no shell and
	// nothing writable, which is what almost every integration wants.
	//
	// "agentic" is the heavier runner, which additionally carries a shell, curl,
	// jq, the standalone octo CLI, dolphin and a scratch workspace at /workspace.
	// It is for an integration built to drive the platform rather than serve it —
	// one whose flows run local commands, invoke other flows, or execute a test
	// suite. Dr. Octo is the first, and the reason it exists.
	//
	// Treat it as a privileged choice rather than a bigger one. A pod with a shell
	// and a runtime it can point at a definition it just wrote is a general
	// execution environment, so the boundary it offers is the pod — not the
	// `cli-run` allow list inside it. Do not grant it to an integration whose
	// definition comes from somewhere you do not trust.
	Runner string `json:"runner,omitempty"`
	// SnapshotID is the version tag (snapshot) to deploy. When the service is wired
	// with a snapshot store (the production path) it is required, and the deploy
	// ships that snapshot's frozen definition rather than the live one. Input only —
	// the resolved snapshot id/tag are recorded in Metadata.
	SnapshotID string `json:"snapshotId,omitempty"`
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
	// SnapshotID is the snapshot (version tag) this deployment was created from, and
	// Tag its human-facing label, captured at deploy/rollout time. Empty on
	// deployments created before version tags existed.
	SnapshotID string `json:"snapshotId,omitempty"`
	Tag        string `json:"tag,omitempty"`
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
