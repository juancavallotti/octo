package authz

import (
	"slices"
	"testing"
)

func TestRolesIn(t *testing.T) {
	person := Principal{
		Roles:    []string{RoleMonitor},
		EnvRoles: map[string][]string{"staging": {RoleDeveloper}},
	}
	admin := Principal{Roles: []string{RoleAdmin}}
	machine := Principal{Roles: []string{"platform:runtime"}, Deployment: "d1", Environment: "staging"}
	legacyMachine := Principal{Roles: []string{"platform:runtime"}, Deployment: "d1"}
	key := Principal{Roles: []string{RoleMonitor}, APIKey: "k1", Environment: "staging"}
	// A bound token never carries admin, but if one somehow did it must not
	// carry it out of its environment.
	adminKey := Principal{Roles: []string{RoleAdmin}, APIKey: "k1", Environment: "staging"}

	cases := []struct {
		name        string
		p           Principal
		environment string
		want        []string
	}{
		{"a person in the default", person, "", []string{RoleMonitor}},
		{"a person in the default by name", person, DefaultEnvironment, []string{RoleMonitor}},
		{"a person elsewhere", person, "staging", []string{RoleDeveloper}},
		{"a person with no grant there", person, "qa", nil},
		{"an admin anywhere", admin, "qa", []string{RoleAdmin}},
		{"a machine in its own environment", machine, "staging", []string{"platform:runtime"}},
		{"a machine in the default", machine, "", nil},
		{"a machine elsewhere", machine, "qa", nil},
		{"a legacy machine in the default", legacyMachine, "", []string{"platform:runtime"}},
		{"a legacy machine elsewhere", legacyMachine, "staging", nil},
		{"a key in its own environment", key, "staging", []string{RoleMonitor}},
		{"a key in the default", key, DefaultEnvironment, nil},
		{"an admin key outside its environment", adminKey, "qa", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.RolesIn(tc.environment); !slices.Equal(got, tc.want) {
				t.Errorf("RolesIn(%q) = %v, want %v", tc.environment, got, tc.want)
			}
		})
	}
}
