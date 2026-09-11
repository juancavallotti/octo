package auth

import (
	"fmt"

	"github.com/juancavallotti/octo/iam/internal/user"
)

// What a deployment's own token may reach.
//
// A machine token always carries platform:runtime, which opens the stores a pod
// owns — its key/value namespace, its objects, its agent memory — and nothing
// else. Access says what it carries *besides* that, for the integrations built
// to read the installation they run on rather than only to serve it.
//
// One choice rather than a set of switches. The three are ordered — each is the
// one before it plus more — so a set would only ever be spelled as its widest
// member, and offering it as a set would suggest combinations that do not exist.

// Access names how much of the platform a deployment's token opens.
type Access string

const (
	// AccessBasic is a deployment that serves. It reaches its own stores and
	// nothing of the installation's, which is right for almost everything.
	AccessBasic Access = "basic"
	// AccessDeveloper additionally reads and writes what this platform is for:
	// integrations, their resources, snapshots and dev runs. For an integration
	// that builds or tests other integrations.
	AccessDeveloper Access = "developer"
	// AccessOperator additionally acts on deployments — deploying, rolling out,
	// scaling and removing them. For an integration that runs the installation.
	AccessOperator Access = "operator"
)

// rolesFor is what a machine token of this access carries. platform:runtime is
// in every one of them: whatever else a deployment may do, it is still a pod
// that owns a key/value namespace.
func rolesFor(access Access) ([]user.Role, error) {
	switch access {
	case AccessBasic, "":
		return []user.Role{user.RoleRuntime}, nil
	case AccessDeveloper:
		return []user.Role{user.RoleRuntime, user.RoleDeveloper}, nil
	case AccessOperator:
		return []user.Role{user.RoleRuntime, user.RoleOperator}, nil
	default:
		return nil, fmt.Errorf("%w: %q is not an access level", user.ErrInvalid, string(access))
	}
}

// accessOf reads the access back off a token that was minted with it.
//
// Renewal needs it because a machine token is renewed as what it was, not as
// what its owner may do now — and going through the roles rather than copying
// them across is what stops a token carrying anything this service would not
// mint today.
func accessOf(roles []user.Role) Access {
	for _, held := range roles {
		switch held {
		case user.RoleOperator:
			return AccessOperator
		case user.RoleDeveloper:
			return AccessDeveloper
		}
	}
	return AccessBasic
}

// mayLend reports whether owner may give a deployment this much access.
//
// The rule is containment, not the literal role: somebody may lend a deployment
// no more than they could do themselves. An operator holds no platform:developer
// row and may nonetheless lend developer access, because building is something
// an operator does here — see the orchestrator's policy, which admits operators
// to everything a developer reaches.
//
// Today every deployer satisfies both branches, because lending an identity at
// all already requires operator or admin. It is spelled out anyway: the day that
// requirement widens, this is the check that has to still be true, and rebuilding
// it from scratch then means rediscovering the argument.
func mayLend(owner user.User, access Access) bool {
	switch access {
	case AccessDeveloper:
		return owner.HasRole(user.RoleAdmin) ||
			owner.HasRole(user.RoleOperator) ||
			owner.HasRole(user.RoleDeveloper)
	case AccessOperator:
		return owner.HasRole(user.RoleAdmin) || owner.HasRole(user.RoleOperator)
	default:
		return true
	}
}
