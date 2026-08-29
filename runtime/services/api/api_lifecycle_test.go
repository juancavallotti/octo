package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Close has to stop the background work, and the work is started from a CALLER's
// context that may outlive the module. Binding only to the caller leaves poll
// loops running against a closed transport; this is the test that says so.
func TestCloseStopsSubscriptionsStartedElsewhere(t *testing.T) {
	f := newFake(t, fastPoll())
	newQueueBackend().install(f)
	svc := newTestServices(t, f, nil)

	// Deliberately NOT the module's context: a long-lived caller.
	sub, err := svc.Queues().Subscribe(t.Context(), "orders", func(_ context.Context, _ types.Message) (types.Message, error) {
		return types.Message{}, nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = sub.Close() })

	// Let the loop poll at least once, then close the module.
	waitForPolls(t, f, 1)
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	settled := f.count(http.MethodPost, "/receive")
	time.Sleep(2 * time.Second)
	if after := f.count(http.MethodPost, "/receive"); after != settled {
		t.Fatalf("the subscription polled %d more times after Close; it is still running "+
			"against a closed module", after-settled)
	}
}

// The same for a lease: its renewals are deliberately detached from the call that
// acquired the claim, which must not mean detached from the module.
func TestCloseStopsLeaseRenewals(t *testing.T) {
	f := newFake(t, fullDiscovery())
	newLeaseBackend().install(f)
	svc := newTestServices(t, f, nil)

	lease, ok, err := svc.Leases().Acquire(t.Context(), "job", core.WithLeaseTTL(time.Second))
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A holder gating work on Done has to hear that the claim is over, rather than
	// carrying on against one quietly expiring on the platform.
	select {
	case <-lease.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close when the module shut down")
	}
}

// And for a leadership campaign.
func TestCloseStopsLeadershipCampaigns(t *testing.T) {
	f := newFake(t, fastElection())
	newLeaderBackend().install(f)
	svc := newTestServices(t, f, nil)

	l, err := svc.LeaderElection().Acquire(t.Context(), "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	awaitLeader(t, l, true, "the key was free")

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitLeader(t, l, false, "leadership survived the module that was maintaining it")
}

// waitForPolls blocks until the fake has seen n receive calls.
func waitForPolls(t *testing.T, f *fake, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f.count(http.MethodPost, "/receive") >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the subscription never polled")
}
