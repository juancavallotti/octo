package auth

import (
	"fmt"
	"slices"

	"github.com/juancavallotti/octo/iam/internal/user"
)

// What a deployment's own token may reach.
//
// A machine token always carries platform:runtime, which opens the stores a pod
// owns — its key/value namespace, its objects, its agent memory — and nothing
// else. Access is what it carries *besides* that, for an integration built to
// act on the installation it runs on rather than only to serve requests.
//
// A set and not a choice. The two are independent: an integration that builds
// other integrations and one that operates deployments are different jobs, and
// something can plausibly do both or neither. Offering them as a ladder would
// make "builds, but does not deploy" unsayable in one direction and hand out
// more than was asked for in the other.

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
// platform:runtime is in every one of them: whatever else a deployment may do, it
// is still a pod that owns a key/value namespace. An empty set is that and
// nothing more, which is what almost every deployment gets.
//
// Duplicates are tolerated and an unknown grant is not. The first is a caller
// sending the same thing twice and means nothing; the second is a caller asking
// for something this service cannot give, and answering with a narrower token
// than they asked for would fail somewhere else entirely.
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
// its owner may do now — and going through the roles rather than copying them
// across is what stops a token carrying anything this service would not mint
// today.
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
// The rule is containment, not the literal role: somebody may lend a deployment
// no more than they could do themselves. An operator holds no platform:developer
// row and may nonetheless lend developer access, because building is something an
// operator does here — see the orchestrator's policy, which admits operators to
// everything a developer reaches.
//
// Today every deployer satisfies both, because lending an identity at all already
// requires operator or admin. It is spelled out anyway: the day that requirement
// widens, this is the check that has to still be true, and rebuilding it from
// scratch then means rediscovering the argument.
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

// describe renders a set for a refusal message, so a caller is told what they
// asked for rather than being left to guess which half was refused.
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
