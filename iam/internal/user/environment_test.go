package user

import (
	"slices"
	"testing"
)

func TestIn(t *testing.T) {
	dev := User{
		Roles:    []Role{RoleMonitor},
		EnvRoles: map[string][]Role{"staging": {RoleDeveloper, RoleOperator}},
	}
	admin := User{Roles: []Role{RoleAdmin}}

	cases := []struct {
		name        string
		u           User
		environment string
		want        []Role
	}{
		{"the default by its empty spelling", dev, "", []Role{RoleMonitor}},
		{"the default by name", dev, DefaultEnvironment, []Role{RoleMonitor}},
		{"another environment", dev, "staging", []Role{RoleDeveloper, RoleOperator}},
		{"an environment with no grant", dev, "qa", nil},
		{"an admin anywhere", admin, "qa", []Role{RoleAdmin}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.u.In(tc.environment).Roles
			if !slices.Equal(got, tc.want) {
				t.Errorf("roles = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInScopesDelegation(t *testing.T) {
	dev := User{EnvRoles: map[string][]Role{"staging": {RoleOperator}}}
	if dev.MayDelegate(RoleOperator) {
		t.Error("operator in staging may delegate operator in the default environment")
	}
	if !dev.In("staging").MayDelegate(RoleOperator) {
		t.Error("operator in staging may not delegate operator there")
	}
	if dev.In("staging").EnvRoles != nil {
		t.Error("a scoped user still carries other environments' roles")
	}
}

func TestNormalizeEnvironment(t *testing.T) {
	if NormalizeEnvironment(DefaultEnvironment) != "" || NormalizeEnvironment("") != "" {
		t.Error("the default is not normalized to the empty string")
	}
	if NormalizeEnvironment("staging") != "staging" {
		t.Error("a named environment changed")
	}
}
