package standalone

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// testClock is a hand-wound clock, so a claim can be aged past its deadline
// without a test waiting out a real one.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func newTestClock() *testClock { return &testClock{at: time.Unix(1_700_000_000, 0)} }

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// acquire takes a claim and fails the test if it was refused or errored.
//
//nolint:ireturn // hands back the core.Lease the caller under test returns
func acquire(t *testing.T, l *leases, name string, opts ...core.LeaseOption) core.Lease {
	t.Helper()
	lease, ok, err := l.Acquire(context.Background(), name, opts...)
	if err != nil {
		t.Fatalf("Acquire(%q) error = %v", name, err)
	}
	if !ok {
		t.Fatalf("Acquire(%q) was refused, want granted", name)
	}
	return lease
}

func TestLeaseAcquireTakesAFreeName(t *testing.T) {
	l := newLeases(newTestClock().now)

	lease := acquire(t, l, "orders")
	defer func() { _ = lease.Close() }()

	select {
	case <-lease.Done():
		t.Fatal("a freshly acquired lease reports itself already lost")
	default:
	}
}

// A refusal has to come back on the request path, so this pins that it does not
// wait for the holder: the TTL is a minute and the assertion is a small fraction
// of it, which any implementation that queued behind the holder would blow.
func TestLeaseRefusesAHeldNameWithoutWaiting(t *testing.T) {
	l := newLeases(newTestClock().now)
	held := acquire(t, l, "orders", core.WithLeaseTTL(time.Minute))
	defer func() { _ = held.Close() }()

	start := time.Now()
	lease, ok, err := l.Acquire(context.Background(), "orders")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Acquire on a held name error = %v, want nil: taken is a decision, not a failure", err)
	}
	if ok {
		t.Fatal("Acquire granted a name somebody else holds")
	}
	if lease != nil {
		t.Errorf("a refused Acquire returned a lease %v, want nil", lease)
	}
	if elapsed > time.Second {
		t.Errorf("a refused Acquire took %v, want an immediate answer", elapsed)
	}
}

func TestLeaseReleaseFreesTheName(t *testing.T) {
	l := newLeases(newTestClock().now)

	first := acquire(t, l, "orders")
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second := acquire(t, l, "orders")
	defer func() { _ = second.Close() }()

	select {
	case <-first.Done():
	default:
		t.Error("Done() is open on a released lease, want closed")
	}
}

func TestLeaseCloseIsIdempotent(t *testing.T) {
	l := newLeases(newTestClock().now)
	lease := acquire(t, l, "orders")

	for i := range 3 {
		if err := lease.Close(); err != nil {
			t.Fatalf("Close() #%d error = %v, want nil", i+1, err)
		}
	}
}

// A holder that dies without releasing must not hold the name forever, which is
// the whole reason a lease expires rather than simply locking.
func TestLeaseExpiryDisplacesAStaleHolder(t *testing.T) {
	clock := newTestClock()
	l := newLeases(clock.now)

	stale := acquire(t, l, "orders", core.WithLeaseTTL(time.Minute))
	clock.advance(2 * time.Minute)

	fresh := acquire(t, l, "orders")
	defer func() { _ = fresh.Close() }()

	select {
	case <-stale.Done():
	default:
		t.Error("Done() is open on a displaced lease, want closed: a holder must be able to hear that it lost the claim")
	}
}

// The successor is the case that is easy to get wrong: a displaced holder closing
// late must not delete the entry that now belongs to somebody else.
func TestLeaseCloseDoesNotEvictASuccessor(t *testing.T) {
	clock := newTestClock()
	l := newLeases(clock.now)

	stale := acquire(t, l, "orders", core.WithLeaseTTL(time.Minute))
	clock.advance(2 * time.Minute)
	successor := acquire(t, l, "orders", core.WithLeaseTTL(time.Minute))
	defer func() { _ = successor.Close() }()

	if err := stale.Close(); err != nil {
		t.Fatalf("Close() on a displaced lease error = %v", err)
	}

	if _, ok, err := l.Acquire(context.Background(), "orders"); err != nil || ok {
		t.Errorf("Acquire after a displaced holder closed = (ok %v, err %v), want refused: the successor still holds it", ok, err)
	}
	select {
	case <-successor.Done():
		t.Error("the successor's Done() closed when its predecessor released")
	default:
	}
}

// Renewal is what keeps a slow holder from being displaced by a challenger, so
// this one runs on the real clock and waits out several TTLs.
func TestLeaseRenewalHoldsANamePastItsTTL(t *testing.T) {
	l := newLeases(time.Now)
	const ttl = 30 * time.Millisecond

	held := acquire(t, l, "orders", core.WithLeaseTTL(ttl))
	defer func() { _ = held.Close() }()

	time.Sleep(4 * ttl)

	if _, ok, err := l.Acquire(context.Background(), "orders"); err != nil || ok {
		t.Errorf("Acquire after %v = (ok %v, err %v), want refused: the holder has been renewing", 4*ttl, ok, err)
	}
	select {
	case <-held.Done():
		t.Error("the holder's Done() closed while it was still renewing")
	default:
	}
}

func TestLeaseNamesDoNotCollide(t *testing.T) {
	l := newLeases(newTestClock().now)

	orders := acquire(t, l, "orders")
	defer func() { _ = orders.Close() }()
	shipments := acquire(t, l, "shipments")
	defer func() { _ = shipments.Close() }()
}

// A non-positive TTL is ignored rather than taken literally, matching every other
// option in this codebase — the alternative is a claim that expires instantly.
func TestLeaseTTLFallsBackToTheDefault(t *testing.T) {
	clock := newTestClock()
	l := newLeases(clock.now)

	lease := acquire(t, l, "orders", core.WithLeaseTTL(0))
	defer func() { _ = lease.Close() }()

	clock.advance(core.DefaultLeaseTTL / 2)
	if _, ok, _ := l.Acquire(context.Background(), "orders"); ok {
		t.Error("a lease with TTL 0 expired within the default TTL, want the default applied")
	}
}

func TestServicesCloseReleasesOutstandingLeases(t *testing.T) {
	svc := New(t.TempDir(), core.TraceOptions{})

	lease, ok, err := svc.Leases().Acquire(context.Background(), "orders")
	if err != nil || !ok {
		t.Fatalf("Acquire() = (ok %v, err %v), want granted", ok, err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-lease.Done():
	default:
		t.Error("Done() is open after the module closed, want closed")
	}
}
