package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

func newLeaseFixture(t *testing.T) (*Services, *fake, *leaseBackend) {
	t.Helper()
	f := newFake(t, fullDiscovery())
	b := newLeaseBackend()
	b.install(f)
	return newTestServices(t, f, nil), f, b
}

// Acquire answers now and answers definitively: a name somebody else holds comes
// back ok=false rather than blocking, which is the whole point of a claim as
// distinct from an election.
func TestLeaseAcquireIsExclusiveAndFailsFast(t *testing.T) {
	svc, _, b := newLeaseFixture(t)
	ctx := t.Context()

	lease, ok, err := svc.Leases().Acquire(ctx, "nightly-report")
	if err != nil || !ok {
		t.Fatalf("first Acquire = (ok %v, err %v), want the claim", ok, err)
	}
	t.Cleanup(func() { _ = lease.Close() })

	// A second claim on the same name, from a client that is not the holder.
	second, ok, err := svc.Leases().Acquire(ctx, "nightly-report")
	if err != nil {
		t.Fatalf("second Acquire err = %v, want a decision rather than a failure", err)
	}
	if ok || second != nil {
		t.Fatal("the same name was claimed twice")
	}
	if got := b.holderOf("nightly-report"); got != "test-instance" {
		t.Fatalf("holder = %q, want the acquiring instance", got)
	}
}

// A claim somebody else let expire is claimable again — otherwise a replica that
// died without releasing would take the name out of service for good.
func TestLeaseTakesOverAnExpiredClaim(t *testing.T) {
	svc, _, b := newLeaseFixture(t)
	ctx := t.Context()

	lease, ok, err := svc.Leases().Acquire(ctx, "job", core.WithLeaseTTL(2*time.Second))
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	// Stop renewing, then age the claim past its TTL.
	_ = lease.Close()
	b.advance(10 * time.Second)

	again, ok, err := svc.Leases().Acquire(ctx, "job")
	if err != nil || !ok {
		t.Fatalf("Acquire after expiry = (ok %v, err %v), want the claim", ok, err)
	}
	_ = again.Close()
}

// Done is how a holder hears that the claim is gone. Work under a lease gates on
// it, so a renewal that stops landing has to close it.
func TestLeaseDoneClosesWhenRenewalFails(t *testing.T) {
	svc, _, b := newLeaseFixture(t)

	// A short TTL means a renewal is attempted almost immediately.
	lease, ok, err := svc.Leases().Acquire(t.Context(), "job", core.WithLeaseTTL(time.Second))
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	b.mu.Lock()
	b.renewFails = true
	b.mu.Unlock()

	select {
	case <-lease.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close after the renewal stopped landing")
	}
}

// Close is idempotent, so a deferred Close beside an explicit one is not an error.
func TestLeaseCloseIsIdempotent(t *testing.T) {
	svc, _, _ := newLeaseFixture(t)
	lease, ok, err := svc.Leases().Acquire(t.Context(), "job")
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("first Close = %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}

// A held claim is renewed for as long as it is held, so it outlives its own TTL.
func TestLeaseRenewsWhileHeld(t *testing.T) {
	svc, f, _ := newLeaseFixture(t)
	lease, ok, err := svc.Leases().Acquire(t.Context(), "job", core.WithLeaseTTL(time.Second))
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	defer func() { _ = lease.Close() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.count(http.MethodPost, "/renew") > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the claim was never renewed")
}

// A 409 is the other way an implementer can say "taken", so it reads as a
// decision rather than a failure.
func TestLeaseConflictReadsAsTaken(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("POST /v1/leases/acquire", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "held", http.StatusConflict)
	})
	svc := newTestServices(t, f, nil)

	lease, ok, err := svc.Leases().Acquire(t.Context(), "job")
	if err != nil {
		t.Fatalf("Acquire err = %v, want a decision", err)
	}
	if ok || lease != nil {
		t.Fatal("a 409 was read as a granted claim")
	}
}

// The TTL is clamped into the platform's declared bounds, so a caller's option is
// honoured where it can be and adjusted where it cannot.
func TestLeaseTTLClampedToDeclaredBounds(t *testing.T) {
	l := &leases{minTTL: 5 * time.Second, maxTTL: 60 * time.Second}
	cases := []struct{ in, want time.Duration }{
		{time.Second, 5 * time.Second},
		{30 * time.Second, 30 * time.Second},
		{10 * time.Minute, 60 * time.Second},
	}
	for _, tc := range cases {
		if got := l.clampTTL(tc.in); got != tc.want {
			t.Errorf("clampTTL(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// Rounding a TTL down would publish a deadline earlier than the one the holder
// renews against, so a challenger would find the claim expired while its owner
// still believed it held.
func TestTTLSecondsRoundsUp(t *testing.T) {
	if got := seconds(1500 * time.Millisecond); got != 2 {
		t.Fatalf("seconds(1.5s) = %d, want 2", got)
	}
	if got := seconds(time.Second); got != 1 {
		t.Fatalf("seconds(1s) = %d, want 1", got)
	}
}

// A blip is not a lost claim. Renewing three times per TTL exists so two may be
// lost before the claim is at risk, and a runtime that gave up on the first
// refused connection would hand its work to another replica over nothing.
func TestLeaseSurvivesATransientRenewalFailure(t *testing.T) {
	svc, _, b := newLeaseFixture(t)
	b.mu.Lock()
	b.renewBlips = 1
	b.mu.Unlock()

	lease, ok, err := svc.Leases().Acquire(t.Context(), "job", core.WithLeaseTTL(3*time.Second))
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	defer func() { _ = lease.Close() }()

	// Past two renewal intervals: the first fails, the second must land.
	select {
	case <-lease.Done():
		t.Fatal("the claim was dropped over a single failed renewal")
	case <-time.After(2500 * time.Millisecond):
	}
	if got := b.attempts(); got < 2 {
		t.Fatalf("renewals attempted = %d, want the loop to have retried", got)
	}
}

// A 409 is definitive: somebody else holds the claim now, so there is nothing to
// retry and Done closes at once rather than after the TTL.
func TestLeaseGivesUpImmediatelyWhenTakenOver(t *testing.T) {
	svc, _, b := newLeaseFixture(t)

	// Nine seconds of TTL means renewals every three, so a Done that closes inside
	// five could only have come from reading the 409 as definitive — waiting the
	// claim out would take nine.
	lease, ok, err := svc.Leases().Acquire(t.Context(), "job", core.WithLeaseTTL(9*time.Second))
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	defer func() { _ = lease.Close() }()
	b.mu.Lock()
	b.renewFails = true
	b.mu.Unlock()

	select {
	case <-lease.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close promptly after the platform said the claim was taken " +
			"over; a definitive answer must not wait out the TTL")
	}
}
