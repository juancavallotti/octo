package auth

import (
	"fmt"
	"slices"

	"github.com/juancavallotti/octo/iam/internal/user"
)

// What a deployment's own token may reach.
//
// A machine token always carries platform:runtime, which opens the stores the
// deployment owns and nothing else. Access is what it carries *besides* that.
//
// A set and not a ladder: building integrations and operating deployments are
// independent, and something can do both or neither.

// Access names one thing a deployment's token opens beyond its own stores.
type Access string

const (
	// AccessDeveloper reads and writes what this platform is for: integrations,
	// their resources, snapshots and dev runs. For an integration that builds or
	// tests other integrations.
	AccessDeveloper Access = "developer"
	// AccessOperator acts on deployments — deploying, rolling out, scaling and
	// removing them. For an integration that operates this installation.
	AccessOperator Access = "operator"
)

// accessRole is the role each grant adds. The map is the whole definition of what
// access means, so a grant nobody listed here cannot quietly become a role.
var accessRole = map[Access]user.Role{
	AccessDeveloper: user.RoleDeveloper,
	AccessOperator:  user.RoleOperator,
}

// rolesFor is what a machine token with this access carries.
//
// platform:runtime is in every one of them, and an empty set is that and nothing
// more. Duplicates are tolerated; an unknown grant is refused rather than answered
// with a narrower token than the caller asked for.
func rolesFor(access []Access) ([]user.Role, error) {
	roles := []user.Role{user.RoleRuntime}
	for _, a := range access {
		role, ok := accessRole[a]
		if !ok {
			return nil, fmt.Errorf("%w: %q is not an access grant", user.ErrInvalid, string(a))
		}
		if !slices.Contains(roles, role) {
			roles = append(roles, role)
		}
	}
	return roles, nil
}

// accessOf reads the access back off a token that was minted with it.
//
// Renewal needs it because a machine token is renewed as what it was, not as what
// its owner may do now. Reading the access back rather than copying the roles
// across stops a token carrying anything this service would not mint today.
func accessOf(roles []user.Role) []Access {
	var access []Access
	for grant, role := range accessRole {
		if slices.Contains(roles, role) {
			access = append(access, grant)
		}
	}
	// Ordered, so a renewed token's claims do not depend on map iteration.
	slices.Sort(access)
	return access
}

// mayLend reports whether owner may give a deployment this much access.
//
// The rule is containment, not the literal role: somebody may lend a deployment no
// more than they could do themselves. An operator holds no platform:developer row
// and may nonetheless lend developer access, because an operator reaches
// everything a developer does.
func mayLend(owner user.User, access []Access) bool {
	for _, a := range access {
		switch a {
		case AccessDeveloper:
			if !owner.HasRole(user.RoleAdmin) && !owner.HasRole(user.RoleOperator) &&
				!owner.HasRole(user.RoleDeveloper) {
				return false
			}
		case AccessOperator:
			if !owner.HasRole(user.RoleAdmin) && !owner.HasRole(user.RoleOperator) {
				return false
			}
		}
	}
	return true
}

// describe renders a set for a refusal message, so the refusal names what was
// asked for.
func describe(access []Access) string {
	if len(access) == 0 {
		return "no"
	}
	out := make([]string, 0, len(access))
	for _, a := range access {
		out = append(out, string(a))
	}
	return fmt.Sprint(out)
}
