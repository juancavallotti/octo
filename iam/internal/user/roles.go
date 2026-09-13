package user

// The catalogue of what a platform user can be granted. Roles are stored in
// user_roles, granted and revoked through the user routes, and read back as part
// of a User.
//
// The four are coarse, and meant to be split later into finer grants without any
// of them being renamed. That is also why Role is a string and the column is a
// varchar: adding one is an edit to this file rather than a schema change.

// Role is a grantable platform role. A named type rather than a bare string so a
// user id and a role cannot be passed to the same function in the wrong order and
// still compile.
type Role string

// Namespaced with `platform:` because these govern the platform itself, leaving
// room for a later per-integration or per-folder namespace.
const (
	// RoleAdmin can do everything, including granting and revoking roles. It is
	// the only role that can create another admin, which is why the first user to
	// sign in is given it — see EnsureFirstAdmin.
	RoleAdmin Role = "platform:admin"
	// RoleMonitor can read the monitoring surfaces: logs, traces, metrics, alerts.
	RoleMonitor Role = "platform:monitor"
	// RoleDeveloper can use the development surfaces: the editor, dev runs, tests.
	RoleDeveloper Role = "platform:developer"
	// RoleOperator can create and manage deployments.
	RoleOperator Role = "platform:operator"

	// RoleRuntime is what a deployed integration's own token carries, and the one
	// role no person is ever granted: it is absent from allRoles below, so ValidRole
	// refuses it and no grant can write it.
	//
	// A machine holding it reaches the few routes a running integration needs — its
	// key/value store, its frozen resources, its agent memory — and nothing else,
	// whoever it was minted on behalf of.
	RoleRuntime Role = "platform:runtime"
)

// allRoles is the catalogue in presentation order. Unexported and copied by
// AllRoles rather than exported directly, because a package-level slice is
// writable by anyone who can see it.
var allRoles = []Role{RoleAdmin, RoleMonitor, RoleDeveloper, RoleOperator}

// AllRoles returns every role in the catalogue, in presentation order.
func AllRoles() []Role {
	out := make([]Role, len(allRoles))
	copy(out, allRoles)
	return out
}

// ValidRole reports whether r is in the catalogue. Every write path checks it, so
// the catalogue and not the database is what constrains the column.
func ValidRole(r Role) bool {
	for _, known := range allRoles {
		if known == r {
			return true
		}
	}
	return false
}

// DescribeRole returns a one-line description of r, or "" for a role outside the
// catalogue. GET /roles answers with it.
func DescribeRole(r Role) string {
	switch r {
	case RoleAdmin:
		return "Full access, including granting and revoking roles."
	case RoleMonitor:
		return "Access to the monitoring features: logs, traces, metrics and alerts."
	case RoleDeveloper:
		return "Access to the development features: the editor, dev runs and tests."
	case RoleOperator:
		return "Create and manage deployments."
	default:
		return ""
	}
}
