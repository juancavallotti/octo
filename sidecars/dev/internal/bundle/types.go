// Package bundle is the dev sidecar's client for the one thing it fetches: the
// workspace bundle for its dev run, served by the orchestrator at
// GET /devruns/{id}/bundle.
//
// The pull direction is deliberate. The orchestrator is already the system of
// record for an integration's definition and resources, so a dev run that reads
// from it directly has one copy of the truth; pushing the config through the BFF
// instead would mean carrying YAML across three hops and reconciling two copies
// of it. The trigger for a reload therefore carries no payload at all — it just
// says "go look again".
package bundle

// Resource is one file the integration declares: an env file or a template. Name
// is the declared resource id and may contain '/' (a future feature uploads zip
// bundles that keep their relative paths), so it is treated as a relative path
// under the workspace and never as a URL path segment.
type Resource struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// Bundle is everything a dev run needs on disk: the integration's definition and
// the resources it declares. Generation is an opaque marker the orchestrator
// stamps, carried only so logs and /status can say which generation is live —
// the sidecar never interprets it.
type Bundle struct {
	Definition string     `json:"definition"`
	Resources  []Resource `json:"resources"`
	Generation string     `json:"generation"`
}
