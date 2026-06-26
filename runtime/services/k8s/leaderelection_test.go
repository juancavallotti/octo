package k8s

import (
	"regexp"
	"testing"
)

// dns1123 approximates the validity rule for a Lease object name: a lowercase
// DNS-1123 subdomain.
var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

func TestLeaseNameIsValidAndDeterministic(t *testing.T) {
	// A key with characters illegal in an object name must still yield a valid name.
	name := leaseName("Dep-ABC_123", "cron:My Flow/tick!")
	if !dns1123.MatchString(name) {
		t.Fatalf("lease name %q is not a valid DNS-1123 name", name)
	}
	if len(name) > 253 {
		t.Fatalf("lease name too long: %d", len(name))
	}
	// Same inputs -> same name (so every replica targets one Lease).
	if again := leaseName("Dep-ABC_123", "cron:My Flow/tick!"); again != name {
		t.Fatalf("lease name not deterministic: %q vs %q", name, again)
	}
}

func TestLeaseNameDistinguishesKeys(t *testing.T) {
	a := leaseName("dep", "key-a")
	b := leaseName("dep", "key-b")
	if a == b {
		t.Fatalf("different keys produced the same lease name: %q", a)
	}
}

func TestLeaseNameDistinguishesDeployments(t *testing.T) {
	a := leaseName("dep-1", "key")
	b := leaseName("dep-2", "key")
	if a == b {
		t.Fatalf("different deployments produced the same lease name: %q", a)
	}
}
