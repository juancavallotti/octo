// Package agent installs the platform's own agent — Dr. Octo — as an ordinary
// integration, and reports on it.
//
// The agent ships inside this binary (see orchestrator/agent) and is deployed
// through the same path as anything a user builds: an integration, its resources, a
// version tag, a deployment. Nothing here is a second deployment mechanism. That is
// the whole design: tracing, pod logs, scaling, rollback and the deployments stream
// all work on the agent because it is not special.
//
// The one thing this package owns is the *reconciliation* between a bundle shipped
// in a binary and rows in a database that a user is free to edit.
package agent

import "time"

// settingsKey is this feature's row in site_settings.
const settingsKey = "agent"

// The states Status reports.
const (
	// StateNotInstalled — no integration has been created.
	StateNotInstalled = "not_installed"
	// StateInstalled — the integration and a tag exist, but nothing is running.
	StateInstalled = "installed"
	// StateDeployed — a deployment is running the installed tag.
	StateDeployed = "deployed"
	// StateUpdateAvailable — the binary carries a newer bundle than was rolled out.
	StateUpdateAvailable = "update_available"
	// StateFailed — the deployment exists and its workload is not healthy.
	StateFailed = "failed"
)

// What stops an install from proceeding, when something does. Reported rather than
// returned as an error on the read path, so the admin page can explain the blockage
// before anyone presses a button that cannot work.
const (
	// BlockedKubernetes — this orchestrator has no in-cluster access.
	BlockedKubernetes = "kubernetes"
	// BlockedEncryption — no KV_ENCRYPTION_KEY, so no API key can be read back.
	BlockedEncryption = "encryption"
	// BlockedLLMKey — no LLM provider key is stored.
	BlockedLLMKey = "llm_key"
	// BlockedAgenticRunner — this installation configures no agentic runner image,
	// which Dr. Octo does not merely prefer but requires: his `cli-run` allow lists
	// name the standalone octo, dolphin and curl, and an allow-list entry is
	// resolved when the flow is built. On any other image his config does not load.
	BlockedAgenticRunner = "agentic_runner"
)

// agenticRunner is the runner the agent is installed on, as the deployment
// settings spell it. A string and not deployment's own constant because the
// settings field is a string: what travels is the JSON value, and naming it here
// keeps the two mentions below from drifting apart.
const agenticRunner = "agentic"

// llmKeySecret is the platform secret the agent's provider key is written to. The
// deployment binds LLM_API_KEY to it by name, so the key itself never enters a
// deployment record.
//
// UPPER_SNAKE_CASE because that is what a platform secret is: not a Kubernetes
// Secret object of its own, but a data key inside the one shared `octo-secrets`
// Secret, and the catalogue constrains those to the intersection of a valid env
// var name and a valid Secret data key. A name in any other shape is refused —
// which is what the first install of this agent did. TestLLMKeySecretIsAValidPlatformSecretName
// holds it to that rule.
//
// It is deliberately an ordinary platform secret, visible in the secrets list
// alongside every other one, because the agent is deployed through the ordinary
// path and this is how that path carries a credential. Uninstalling with purge
// removes it.
const llmKeySecret = "OCTO_AGENT_LLM_KEY"

// Environment variables the installer binds on the agent's deployment. They mirror
// the app's own env block; a change on either side without the other fails the
// deploy with the missing name, which is the right place to find out.
const (
	envConnectorType = "LLM_CONNECTOR_TYPE"
	envModel         = "LLM_MODEL"
	envAPIKey        = "LLM_API_KEY"
	envOrchestrator  = "ORCHESTRATOR_URL"
)

// stored is the jsonb shape in site_settings.value: what the orchestrator knows
// about an install it performed. Unexported — the read model is Status.
type stored struct {
	IntegrationID string `json:"integrationId,omitempty"`
	DeploymentID  string `json:"deploymentId,omitempty"`
	Slug          string `json:"slug,omitempty"`
	InternalURL   string `json:"internalUrl,omitempty"`

	// InstalledDigest is the bundle as of the last install or roll-out, and
	// InstalledTag the version tag it was published under. Comparing the digest with
	// the running binary's is what "an update is available" means.
	InstalledDigest string `json:"installedDigest,omitempty"`
	InstalledTag    string `json:"installedTag,omitempty"`
	SnapshotID      string `json:"snapshotId,omitempty"`

	// Tracing is the setting the last install or toggle applied. Kept here as well
	// as on the deployment so the admin page can render the toggle without a cluster
	// round trip, and so it survives a redeploy.
	Tracing bool `json:"tracing,omitempty"`

	InstalledAt time.Time `json:"installedAt,omitzero"`
	UpdatedAt   time.Time `json:"updatedAt,omitzero"`
}

// Status is what the admin page renders. It carries no key material and no
// definition — only what is needed to decide which button to offer.
type Status struct {
	State         string
	IntegrationID string
	DeploymentID  string
	InternalURL   string
	InstalledTag  string

	// BundleDigest is what this binary ships; InstalledDigest what was last rolled
	// out. They differ exactly when an update is available.
	BundleDigest    string
	InstalledDigest string
	UpdateAvailable bool

	// Edited reports that the integration's definition or its skills no longer match
	// the bundle they were installed from — someone changed the agent, which is a
	// supported thing to do. It exists so a roll-out can warn that it will replace
	// those changes rather than discovering it afterwards.
	Edited bool

	Tracing bool

	// Blocked names what prevents installing or rolling out, or is empty.
	Blocked string

	// DeploymentStatus is the deployment's own phase, and Reason its failure
	// message when it has one.
	DeploymentStatus string
	Reason           string
}
