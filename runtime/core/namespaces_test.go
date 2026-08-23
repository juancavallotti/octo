package core_test

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

func TestVolatileNamespace(t *testing.T) {
	cases := map[string]string{
		core.NamespaceSystem: core.NamespaceSystemVolatile,
		core.NamespaceUser:   core.NamespaceUserVolatile,
		"tenant":             "tenant_volatile",
	}
	for in, want := range cases {
		if got := core.VolatileNamespace(in); got != want {
			t.Errorf("VolatileNamespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNamespaceClassification(t *testing.T) {
	cases := []struct {
		namespace string
		volatile  bool
		secret    bool
	}{
		{core.NamespaceUser, false, false},
		{core.NamespaceSystem, false, false},
		{core.NamespaceUserVolatile, true, false},
		{core.NamespaceSystemVolatile, true, false},
		{core.NamespaceUserSecrets, false, true},
		{core.NamespaceSystemSecrets, false, true},
	}
	for _, tc := range cases {
		if got := core.IsVolatileNamespace(tc.namespace); got != tc.volatile {
			t.Errorf("IsVolatileNamespace(%q) = %v, want %v", tc.namespace, got, tc.volatile)
		}
		if got := core.IsSecretNamespace(tc.namespace); got != tc.secret {
			t.Errorf("IsSecretNamespace(%q) = %v, want %v", tc.namespace, got, tc.secret)
		}
	}
}

// The two tiers must stay disjoint: a namespace that is both secret and volatile
// would be a credential in a store that is allowed to drop it and does not encrypt.
// Nothing in the runtime composes the two, and this pins that the named constants
// cannot drift into overlapping.
func TestNoNamespaceIsBothSecretAndVolatile(t *testing.T) {
	for _, ns := range []string{
		core.NamespaceUser, core.NamespaceSystem,
		core.NamespaceUserSecrets, core.NamespaceSystemSecrets,
		core.NamespaceUserVolatile, core.NamespaceSystemVolatile,
	} {
		if core.IsSecretNamespace(ns) && core.IsVolatileNamespace(ns) {
			t.Errorf("namespace %q is both secret and volatile", ns)
		}
	}
}
