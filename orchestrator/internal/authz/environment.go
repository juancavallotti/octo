package authz

import "slices"

// DefaultEnvironment is the environment every installation has: the release
// namespace. Tokens spell it as the empty string; "octo" names it the same.
const DefaultEnvironment = "octo"

// Bound reports whether the token acts in exactly one environment: a machine's,
// minted for a deployment there, or one exchanged for a key bound there. Binding
// is read off those claims and never off Environment, so a bound token minted
// before environments existed — which names none — is bound to the default
// rather than to nothing.
func (p Principal) Bound() bool {
	return p.Deployment != "" || p.APIKey != ""
}

// RolesIn returns the roles this principal holds in environment ("" or "octo"
// for the default).
//
// A bound token holds its Roles in its own environment and nothing anywhere
// else, so a leaked pod or key token reaches no further than the environment it
// was minted for. A person holds Roles in the default and EnvRoles elsewhere, and
// an administrator — platform:admin is only ever granted in the default — holds
// admin in every environment.
func (p Principal) RolesIn(environment string) []string {
	if environment == DefaultEnvironment {
		environment = ""
	}
	if p.Bound() {
		if environment != p.Environment {
			return nil
		}
		return p.Roles
	}
	if environment == "" {
		return p.Roles
	}
	roles := slices.Clone(p.EnvRoles[environment])
	if slices.Contains(p.Roles, RoleAdmin) && !slices.Contains(roles, RoleAdmin) {
		roles = append(roles, RoleAdmin)
	}
	return roles
}
