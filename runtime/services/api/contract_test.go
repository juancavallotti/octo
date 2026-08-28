package api

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services/servicestest"
)

// The shared contract, run against the api module.
//
// It matters most here: this module's KV and leases are only as correct as the
// platform behind them, and the same suite is what the conformance harness will
// point at a real implementation. Running it against the reference fake is how we
// know the harness is checking the right thing.
func TestKVContract(t *testing.T) {
	svc, _, _ := newKVFixture(t)
	servicestest.KVContract(t, core.NamespaceUser, svc.KV())
}

func TestLeasesContract(t *testing.T) {
	svc, _, _ := newLeaseFixture(t)
	servicestest.LeasesContract(t, svc.Leases())
}
