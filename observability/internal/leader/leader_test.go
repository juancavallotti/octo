package leader

import (
	"testing"
)

// Off-cluster there is no API server to ask and no second replica to compete
// with, so the elector acts. That is not a degraded stand-in: a single process
// competing with nobody is the leader by definition, and it is what makes
// `task observability:run` and every test here work without a cluster.
func TestWithoutAPodIdentityThisProcessActs(t *testing.T) {
	t.Setenv(podNameVar, "")
	t.Setenv(podNamespaceVar, "")

	e, err := New(t.Context())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !e.IsLeader() {
		t.Error("a process with nobody to compete with did not act")
	}
	if e.Identity() == "" {
		t.Error("the elector has no identity to log")
	}
}

// A pod that has the downward-API variables and cannot reach the API server is
// misconfigured. Electing itself anyway would put two evaluators on one
// installation, each opening incidents and sending mail, so it fails loudly.
func TestAPodThatCannotReachTheAPIServerRefusesToElectItself(t *testing.T) {
	t.Setenv(podNameVar, "observability-0")
	t.Setenv(podNamespaceVar, "octo")
	// rest.InClusterConfig reads these; with the identity set and no token
	// mounted, it must fail rather than fall back.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	if _, err := New(t.Context()); err == nil {
		t.Error("a pod with no cluster to talk to elected itself")
	}
}

// A replica that is campaigning has not won yet, and must not act in the
// meantime — the tick it would be serving belongs to whoever holds the lease.
func TestACampaigningReplicaDoesNotActUntilItWins(t *testing.T) {
	e := &Elector{identity: "observability-0"}
	if e.IsLeader() {
		t.Fatal("a replica acted before winning the lease")
	}
	e.leader.Store(true)
	if !e.IsLeader() {
		t.Fatal("a replica that won the lease did not act")
	}
	e.leader.Store(false)
	if e.IsLeader() {
		t.Fatal("a replica kept acting after losing the lease")
	}
}
