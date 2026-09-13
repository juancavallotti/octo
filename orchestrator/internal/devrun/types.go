// Package devrun runs an integration from its live definition as a pod of its own,
// one per (user, integration).
//
// It is the one feature module with no repo.go, and that absence is the design.
// There is no dev_runs table: a dev run's identity is *derived* from
// (user, integration), its ownership is carried by labels, its last use by one
// annotation, and its existence is its status. So the state seam is the Kubernetes
// client rather than a repository, and "is something running for this integration?"
// is a label lookup against an informer cache that cannot disagree with what is
// actually running.
//
// The consequence: **nothing here remembers a stopped dev run**. Stop deletes the
// workload, and starting again derives the same uuid and the same public host from
// the same pair. A table would only hold a copy of what the cluster already knows.
package devrun

import (
	"encoding/json"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// Resource is one file a dev run's workspace needs: an env file or a template.
// Name is the declared resource id and may contain '/', so the sidecar treats it
// as a relative path under the workspace.
//
// The json tags are the wire contract with the dev sidecar
// (sidecars/dev/internal/bundle), which decodes exactly this shape.
type Resource struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// Bundle is everything a dev run needs on disk: the integration's saved definition
// and the resources it declares.
//
// Generation marks which revision of that content this is. The sidecar never
// interprets it — it exists so a log line or a status response can say which
// generation is live, which is the difference between "the reload was a no-op" and
// "the reload did not happen".
type Bundle struct {
	Definition string     `json:"definition"`
	Resources  []Resource `json:"resources"`
	Generation string     `json:"generation"`
}

// DevRun is one live dev run, read back from the cluster. Every field comes off the
// workload or is re-derived; none of it is read from a database, because none of it
// is written to one.
type DevRun struct {
	ID            string
	UserID        string
	IntegrationID string
	// Host is the external hostname this run publishes, "" when it serves no HTTP.
	Host string
	// TestURL is Host as a URL: the run's address.
	TestURL string
	// LastActivity is when the run was last reloaded; zero when the annotation is
	// absent, which the reaper reads as "use the creation time instead".
	LastActivity time.Time
	CreatedAt    time.Time
	Detail       kube.Status
	// Sidecar is the sidecar's own view of the run — which generation it applied, when
	// it last pulled, what the runtime's admin port says — passed through as the JSON
	// the sidecar produced. Nil when the sidecar was not asked or did not answer.
	//
	// Opaque rather than re-declared, because the orchestrator interprets none of it
	// and two copies of one schema in two independently versioned modules is how a
	// status surface starts lying.
	Sidecar json.RawMessage

	// tokenHash is the SHA-256 recorded on the workload, used to authorise a sidecar's
	// own requests. Unexported so it cannot leak into a response by being forgotten
	// about — nothing outside this package has a use for it.
	tokenHash string
}

// EnsureResult reports what Ensure did. Created distinguishes starting a run from
// attaching to one that was already there: two requests for the same pair share one
// pod, and the difference is worth reporting rather than hiding.
type EnsureResult struct {
	DevRun
	Created bool
}
