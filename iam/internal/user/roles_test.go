package user

import (
	"slices"
	"testing"
)

func TestValidRole(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{"admin", RoleAdmin, true},
		{"monitor", RoleMonitor, true},
		{"developer", RoleDeveloper, true},
		{"operator", RoleOperator, true},
		{"unknown namespace", Role("octo:admin"), false},
		{"misspelled", Role("platform:admins"), false},
		{"unprefixed", Role("admin"), false},
		{"empty", Role(""), false},
		// Roles arrive from a URL path segment, so the case that matters is the one
		// a caller would get wrong by accident rather than one they invented.
		{"wrong case", Role("Platform:Admin"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidRole(tt.role); got != tt.want {
				t.Errorf("ValidRole(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

// The catalogue is what constrains a varchar column, so every entry in it must be
// grantable and describable — a role added to allRoles and nowhere else would be
// storable and would render with no explanation of what it grants.
func TestEveryCatalogueRoleIsValidAndDescribed(t *testing.T) {
	roles := AllRoles()
	if len(roles) == 0 {
		t.Fatal("the catalogue is empty")
	}
	seen := make(map[Role]struct{}, len(roles))
	for _, r := range roles {
		if !ValidRole(r) {
			t.Errorf("AllRoles() returned %q, which ValidRole() rejects", r)
		}
		if DescribeRole(r) == "" {
			t.Errorf("role %q has no description", r)
		}
		if _, dup := seen[r]; dup {
			t.Errorf("role %q appears twice in the catalogue", r)
		}
		seen[r] = struct{}{}
	}
}

// AllRoles hands out a copy; a caller that sorts or truncates it must not be able
// to change what the next caller sees.
func TestAllRolesReturnsACopy(t *testing.T) {
	first := AllRoles()
	if len(first) == 0 {
		t.Fatal("the catalogue is empty")
	}
	original := first[0]
	first[0] = Role("mutated")

	if second := AllRoles(); second[0] != original {
		t.Errorf("AllRoles()[0] = %q after a caller mutated an earlier result, want %q",
			second[0], original)
	}
}

func TestDescribeUnknownRoleIsEmpty(t *testing.T) {
	if got := DescribeRole(Role("platform:nope")); got != "" {
		t.Errorf("DescribeRole(unknown) = %q, want an empty string", got)
	}
}

func TestReaches(t *testing.T) {
	tests := []struct {
		name string
		held []Role
		want []Role
	}{
		{"admin reaches everything", []Role{RoleAdmin}, []Role{RoleAdmin, RoleOperator, RoleDeveloper, RoleMonitor}},
		{"operator", []Role{RoleOperator}, []Role{RoleOperator, RoleDeveloper, RoleMonitor}},
		{"developer", []Role{RoleDeveloper}, []Role{RoleDeveloper, RoleMonitor}},
		{"monitor", []Role{RoleMonitor}, []Role{RoleMonitor}},
		{"nothing", nil, nil},
		{"monitor and developer", []Role{RoleMonitor, RoleDeveloper}, []Role{RoleDeveloper, RoleMonitor}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := User{Roles: tt.held}
			for _, r := range append(AllRoles(), RoleRuntime) {
				want := slices.Contains(tt.want, r)
				if got := u.Reaches(r); got != want {
					t.Errorf("holding %v, Reaches(%q) = %v, want %v", tt.held, r, got, want)
				}
			}
		})
	}
}

// A key never carries admin, and every role it may carry is a real one.
func TestKeyRoles(t *testing.T) {
	for _, r := range KeyRoles() {
		if !ValidRole(r) {
			t.Errorf("KeyRoles() returned %q, which ValidRole() rejects", r)
		}
	}
	if ValidKeyRole(RoleAdmin) || ValidKeyRole(RoleRuntime) {
		t.Error("admin and runtime must not be carried by a key")
	}
}
