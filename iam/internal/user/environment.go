package user

import "slices"

// DefaultEnvironment is the environment every installation has: the release
// namespace. Tokens and rows spell it as the empty string; "octo" is accepted
// wherever a caller names one and means the same.
const DefaultEnvironment = "octo"

// NormalizeEnvironment maps the default environment's name to the empty string
// that tokens and rows spell it with, and leaves every other name alone.
func NormalizeEnvironment(environment string) string {
	if environment == DefaultEnvironment {
		return ""
	}
	return environment
}

// In returns u as they stand in environment: their roles there, and nothing
// from any other. An administrator stays one everywhere — platform:admin is only
// ever granted in the default environment and reaches every environment — so
// every check written against a User (HasRole, MayDelegate) answers for that
// environment unchanged.
func (u User) In(environment string) User {
	scoped := u
	scoped.EnvRoles = nil
	environment = NormalizeEnvironment(environment)
	if environment == "" {
		return scoped
	}
	scoped.Roles = slices.Clone(u.EnvRoles[environment])
	if u.HasRole(RoleAdmin) && !slices.Contains(scoped.Roles, RoleAdmin) {
		scoped.Roles = append(scoped.Roles, RoleAdmin)
	}
	return scoped
}
