// Package bundle is the dev sidecar's client for the one thing it fetches: the
// workspace bundle for its dev run, served by the orchestrator at
// GET /devruns/{id}/bundle.
//
// It pulls rather than being pushed to: the orchestrator is the system of record
// for an integration's definition and resources, so reading from it directly leaves
// one copy of the truth. A reload trigger therefore carries no payload.
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
// the resources it declares. Generation is an opaque marker carried so logs and
// /status can name the live generation; it is never interpreted here.
type Bundle struct {
	Definition string     `json:"definition"`
	Resources  []Resource `json:"resources"`
	Generation string     `json:"generation"`
}
