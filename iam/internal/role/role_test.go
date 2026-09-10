package role

import "testing"

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{"admin", Admin, true},
		{"monitor", Monitor, true},
		{"developer", Developer, true},
		{"operator", Operator, true},
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
			if got := Valid(tt.role); got != tt.want {
				t.Errorf("Valid(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

// The catalogue is what constrains a varchar column, so every entry in it must be
// describable and valid — a role added to `all` and nowhere else would be
// grantable and would render with no explanation of what it grants.
func TestEveryCatalogueRoleIsValidAndDescribed(t *testing.T) {
	roles := All()
	if len(roles) == 0 {
		t.Fatal("the catalogue is empty")
	}
	seen := make(map[Role]struct{}, len(roles))
	for _, r := range roles {
		if !Valid(r) {
			t.Errorf("All() returned %q, which Valid() rejects", r)
		}
		if Describe(r) == "" {
			t.Errorf("role %q has no description", r)
		}
		if _, dup := seen[r]; dup {
			t.Errorf("role %q appears twice in the catalogue", r)
		}
		seen[r] = struct{}{}
	}
}

// All() hands out a copy; a caller that sorts or truncates it must not be able to
// change what the next caller sees.
func TestAllReturnsACopy(t *testing.T) {
	first := All()
	if len(first) == 0 {
		t.Fatal("the catalogue is empty")
	}
	original := first[0]
	first[0] = Role("mutated")

	if second := All(); second[0] != original {
		t.Errorf("All()[0] = %q after a caller mutated an earlier result, want %q",
			second[0], original)
	}
}

func TestDescribeUnknownRoleIsEmpty(t *testing.T) {
	if got := Describe(Role("platform:nope")); got != "" {
		t.Errorf("Describe(unknown) = %q, want an empty string", got)
	}
}
