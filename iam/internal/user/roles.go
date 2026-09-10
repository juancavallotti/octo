package user

// The catalogue of what a platform user can be granted.
//
// It lives in this package because this package owns roles: they are stored in
// user_roles, granted and revoked through the user routes, and read back as part
// of a User. Pulling the vocabulary out into a package of its own would be a
// layer split rather than a feature one — and the only thing it would buy is an
// import that `auth` already has, since it takes users from here anyway.
//
// The four are coarse on purpose. They are the axes the platform already divides
// along, and they are meant to be split later into finer grants without any of
// them being renamed: a developer who should not see production loses a narrower
// role, not this one. That is also why Role is a string and the column is a
// varchar — adding one is an edit to this file, not a schema change applied by a
// Job.

// Role is a grantable platform role. A named type rather than a bare string so a
// user id and a role cannot be passed to the same function in the wrong order and
// still compile.
type Role string

// Namespaced with `platform:` because these govern the platform itself; a later
// per-integration or per-folder grant is a different namespace, and keeping the
// prefix now is what leaves room for it.
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

// ValidRole reports whether r is in the catalogue. Every write path checks this,
// so the catalogue — and not the database — is what constrains the column: a
// misspelled role is refused rather than stored as a grant nothing will match.
func ValidRole(r Role) bool {
	for _, known := range allRoles {
		if known == r {
			return true
		}
	}
	return false
}

// DescribeRole returns a one-line description of r, or "" for a role outside the
// catalogue. It is here rather than in a UI layer so the catalogue's meaning
// travels with the catalogue, and GET /roles can answer with it.
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
