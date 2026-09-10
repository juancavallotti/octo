package projectfile

import "testing"

// The cases here are the runtime's rules restated. When one of them changes in
// runtime/core/runtime/config.go, this table is what should fail first.
func TestClassify(t *testing.T) {
	cases := []struct {
		path string
		want Role
	}{
		{"orders.yaml", RoleConfig},
		{"orders.yml", RoleConfig},
		{"ORDERS.YAML", RoleConfig},
		{"integration.yaml", RoleConfig},

		{"orders_test.yaml", RoleTest},
		{"orders_test.yml", RoleTest},

		// the runtime matches _test case-sensitively, so this is config there
		// and must stay config here
		{"orders_TEST.yaml", RoleConfig},

		// a directory config is non-recursive, which is what makes anything
		// nested a resource without being declared as one
		{"templates/welcome.tmpl", RoleResource},
		{"skills/refunds.md", RoleResource},
		{"nested/orders.yaml", RoleResource},
		{"nested/orders_test.yaml", RoleResource},
		{".octo/editor-meta.json", RoleResource},

		{".env", RoleResource},
		{".env.dev", RoleResource},
		{"README", RoleResource},
		{"notes.txt", RoleResource},
	}

	for _, c := range cases {
		if got := Classify(c.path); got != c.want {
			t.Errorf("Classify(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"orders_test.yaml": true,
		"orders_test.yml":  true,
		"orders_test":      true,
		"orders.yaml":      false,
		"orders_TEST.yaml": false,
		"test.yaml":        false,
		"_test.yaml":       true,
	}

	for name, want := range cases {
		if got := IsTestFile(name); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", name, got, want)
		}
	}
}
