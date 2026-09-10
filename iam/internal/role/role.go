// Package role is the catalogue of what a platform user can be granted. It is
// deliberately dependency-free — no database, no HTTP — so every other package
// can name a role without dragging anything along, and so the catalogue is one
// list in one file rather than a set of strings scattered across call sites.
//
// The four roles here are coarse on purpose. They are the axes the platform
// already divides along, and they are meant to be split later into finer grants
// without any of them being renamed: a `platform:developer` who should not see
// production loses a narrower role, not this one. That is also why Role is a
// string type and the storage column is a varchar — adding a role is an edit to
// this file, not a schema change applied by a Job.
package role

// The catalogue. Namespaced with `platform:` because these govern the platform
// itself; a later per-integration or per-folder grant is a different namespace,
// and keeping the prefix now is what leaves room for it.
const (
	// Admin can do everything, including granting and revoking roles. It is the
	// only role that can create another admin, which is why the first user to
	// sign in is given it — see the auth package.
	Admin Role = "platform:admin"
	// Monitor can read the monitoring surfaces: logs, traces, metrics, alerts.
	Monitor Role = "platform:monitor"
	// Developer can use the development surfaces: the editor, dev runs, tests.
	Developer Role = "platform:developer"
	// Operator can create and manage deployments.
	Operator Role = "platform:operator"
)

// Role is a grantable platform role. A named type rather than a bare string so a
// user id and a role cannot be passed to the same function in the wrong order and
// still compile.
type Role string

// all is the catalogue in the order it is presented. Unexported and copied by
// All() rather than exported directly, because a package-level slice is writable
// by anyone who can see it.
var all = []Role{Admin, Monitor, Developer, Operator}

// All returns every role in the catalogue, in presentation order.
func All() []Role {
	out := make([]Role, len(all))
	copy(out, all)
	return out
}

// Valid reports whether r is in the catalogue. Every write path checks this, so
// the catalogue — and not the database — is what constrains the column: a
// misspelled role is refused rather than stored as a grant nothing will ever
// match.
func Valid(r Role) bool {
	for _, known := range all {
		if known == r {
			return true
		}
	}
	return false
}

// Describe returns a one-line description of r, or "" for a role outside the
// catalogue. It is here rather than in a UI layer so the catalogue's meaning
// travels with the catalogue, and GET /roles can answer with it.
func Describe(r Role) string {
	switch r {
	case Admin:
		return "Full access, including granting and revoking roles."
	case Monitor:
		return "Access to the monitoring features: logs, traces, metrics and alerts."
	case Developer:
		return "Access to the development features: the editor, dev runs and tests."
	case Operator:
		return "Create and manage deployments."
	default:
		return ""
	}
}
