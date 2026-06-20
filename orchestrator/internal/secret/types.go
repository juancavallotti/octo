// Package secret is the orchestrator feature module for cluster-wide secrets: a
// shared pool of named values that deployments reference as environment variables.
// Values are write-only — set or overwritten, never read back — and live in a
// single Kubernetes Secret (internal/kube). This table-backed catalog records only
// the names and their timestamps so the UI can list them; the value never touches
// the database.
package secret

import (
	"regexp"
	"time"
)

// Secret is a catalog entry: a cluster secret's name and its timestamps. It never
// carries the value — there is no path in this package that reads a value.
type Secret struct {
	Name        string
	CreatedAt   time.Time
	LastUpdated time.Time
}

// maxNameLen bounds a secret name. 253 is the Kubernetes Secret data-key limit; an
// env var name has no hard limit, so the k8s constraint is the binding one.
const maxNameLen = 253

// nameRe is the intersection of a valid pod env-var name and a valid Kubernetes
// Secret data key: an uppercase identifier. The name is used both as the env var
// the value binds to and as the key within the shared Secret, so it must satisfy
// both — this regexp is a strict subset of each.
var nameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// validName reports whether name is usable as both an env var name and a Secret
// data key.
func validName(name string) bool {
	return len(name) <= maxNameLen && nameRe.MatchString(name)
}
