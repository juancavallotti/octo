package standalone

import (
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services/servicestest"
)

// The shared contract, run against the standalone module. A map under a mutex is
// not a stand-in for the real thing but the complete and exact implementation for
// a single process, so it has to satisfy the same rules as the other two.
func TestKVContract(t *testing.T) {
	servicestest.KVContract(t, core.NamespaceUser, newStore(t.TempDir()))
}

// Also on disk, where the same rules have to survive serialization.
func TestKVContractInMemory(t *testing.T) {
	servicestest.KVContract(t, core.NamespaceUser, newStore(""))
}

func TestLeasesContract(t *testing.T) {
	servicestest.LeasesContract(t, newLeases(time.Now))
}
