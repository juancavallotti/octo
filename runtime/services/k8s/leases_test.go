package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	coordv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testNamespace = "octo-test"
	testIdentity  = "runtime-0"
	testDeploy    = "dep-1"
	// testClaim is the name every test here claims. One name is enough: what
	// varies between them is the state of the object, not which object it is.
	testClaim = "orders"
)

// fixedClock is a hand-wound clock, so a claim can be aged past its deadline
// without a test waiting out a real one.
type fixedClock struct{ at time.Time }

func newFixedClock() *fixedClock { return &fixedClock{at: time.Unix(1_700_000_000, 0).UTC()} }

func (c *fixedClock) now() time.Time { return c.at }

// newTestLeases builds the module against a fake API server, and returns the
// clientset so a test can inspect or corrupt what was written.
func newTestLeases(t *testing.T, clock *fixedClock, objects ...runtime.Object) (*leases, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	return newLeases(client.CoordinationV1(), testNamespace, testIdentity, testDeploy, clock.now), client
}

// storedClaim reads back the object testClaim wrote.
func storedClaim(t *testing.T, client *fake.Clientset) *coordv1.Lease {
	t.Helper()
	record, err := client.CoordinationV1().Leases(testNamespace).
		Get(context.Background(), claimName(testDeploy, testClaim), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading back the claim on %q: %v", testClaim, err)
	}
	return record
}

func TestClaimAcquireWritesTheHolderAndDuration(t *testing.T) {
	clock := newFixedClock()
	l, client := newTestLeases(t, clock)

	lease, ok, err := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(time.Minute))
	if err != nil || !ok {
		t.Fatalf("Acquire() = (ok %v, err %v), want granted", ok, err)
	}
	defer func() { _ = lease.Close() }()

	record := storedClaim(t, client)
	if got := holderOf(record); got != testIdentity {
		t.Errorf("holderIdentity = %q, want %q", got, testIdentity)
	}
	if record.Spec.LeaseDurationSeconds == nil || *record.Spec.LeaseDurationSeconds != 60 {
		t.Errorf("leaseDurationSeconds = %v, want 60", record.Spec.LeaseDurationSeconds)
	}
	if record.Spec.RenewTime == nil || !record.Spec.RenewTime.Time.Equal(clock.now()) {
		t.Errorf("renewTime = %v, want %v", record.Spec.RenewTime, clock.now())
	}
}

// A live holder is refused, and refused as a decision rather than as an error —
// the caller has a different path to take and needs to know which one.
// A coordination Lease has no unit finer than a second, so a duration that does
// not divide evenly has to round UP. Rounding down would publish a deadline
// earlier than the one the holder renews against, and a challenger reading the
// object would find the claim expired while its owner still believed it held.
func TestClaimDurationRoundsUpToWholeSeconds(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
		want int32
	}{
		{name: "exact", ttl: 2 * time.Second, want: 2},
		{name: "fractional rounds up", ttl: 1500 * time.Millisecond, want: 2},
		// core.NewLeaseConfig raises anything shorter to core.MinLeaseTTL, so the
		// duration written can never be zero — which would read as expired the
		// instant it was written.
		{name: "sub-second is raised, never zero", ttl: 30 * time.Millisecond, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, client := newTestLeases(t, newFixedClock())
			lease, ok, err := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(tc.ttl))
			if err != nil || !ok {
				t.Fatalf("Acquire() = (ok %v, err %v), want granted", ok, err)
			}
			defer func() { _ = lease.Close() }()

			got := storedClaim(t, client).Spec.LeaseDurationSeconds
			if got == nil {
				t.Fatalf("leaseDurationSeconds for a %v ttl is unset, want %d", tc.ttl, tc.want)
			}
			if *got != tc.want {
				t.Errorf("leaseDurationSeconds for a %v ttl = %d, want %d", tc.ttl, *got, tc.want)
			}
		})
	}
}

func TestClaimRefusesALiveHolder(t *testing.T) {
	clock := newFixedClock()
	l, _ := newTestLeases(t, clock)

	first, ok, err := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(time.Minute))
	if err != nil || !ok {
		t.Fatalf("first Acquire() = (ok %v, err %v), want granted", ok, err)
	}
	defer func() { _ = first.Close() }()

	second, ok, err := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(time.Minute))
	if err != nil {
		t.Fatalf("Acquire on a held name error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Acquire granted a name a live holder has")
	}
	if second != nil {
		t.Errorf("a refused Acquire returned a lease %v, want nil", second)
	}
}

// The object outlives the replica that wrote it, so a claim left behind by a dead
// holder has to be takeable — otherwise one crash takes a conversation out of
// service permanently.
func TestClaimTakesOverAnExpiredHolder(t *testing.T) {
	clock := newFixedClock()
	l, client := newTestLeases(t, clock)

	stale, ok, _ := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(time.Minute))
	if !ok {
		t.Fatal("first Acquire was refused")
	}
	_ = stale
	clock.at = clock.at.Add(2 * time.Minute)

	successor, ok, err := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(time.Minute))
	if err != nil || !ok {
		t.Fatalf("Acquire on an expired claim = (ok %v, err %v), want granted", ok, err)
	}
	defer func() { _ = successor.Close() }()

	if got := storedClaim(t, client).Spec.RenewTime; got == nil || !got.Time.Equal(clock.now()) {
		t.Errorf("renewTime after takeover = %v, want %v", got, clock.now())
	}
}

// A record with no renewal time names no moment its expiry could be measured
// from, so it must read as expired rather than as held forever.
func TestClaimTreatsARecordWithNoRenewTimeAsExpired(t *testing.T) {
	clock := newFixedClock()
	other := "someone-else"
	l, _ := newTestLeases(t, clock, &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: claimName(testDeploy, testClaim), Namespace: testNamespace},
		Spec:       coordv1.LeaseSpec{HolderIdentity: &other},
	})

	lease, ok, err := l.Acquire(context.Background(), testClaim)
	if err != nil || !ok {
		t.Fatalf("Acquire on a claim with no renewTime = (ok %v, err %v), want granted", ok, err)
	}
	_ = lease.Close()
}

// Two replicas racing one dead holder must not both win. The API server settles
// it by rejecting the second Update, and that rejection is a refusal here — not
// an error, because nothing went wrong.
func TestClaimLosingTheRaceForAnExpiredHolderIsARefusal(t *testing.T) {
	clock := newFixedClock()
	other := "someone-else"
	stamp := metav1.NewMicroTime(clock.now().Add(-time.Hour))
	seconds := int32(60)
	l, client := newTestLeases(t, clock, &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: claimName(testDeploy, testClaim), Namespace: testNamespace},
		Spec: coordv1.LeaseSpec{
			HolderIdentity: &other, RenewTime: &stamp, LeaseDurationSeconds: &seconds,
		},
	})
	client.PrependReactor("update", "leases",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"},
				claimName(testDeploy, testClaim), errClaimTakenOver)
		})

	lease, ok, err := l.Acquire(context.Background(), testClaim)
	if err != nil {
		t.Fatalf("Acquire losing a takeover race error = %v, want nil: another replica winning is not a failure", err)
	}
	if ok {
		t.Fatal("both replicas were granted a claim on one name")
	}
	if lease != nil {
		t.Errorf("a refused Acquire returned a lease %v, want nil", lease)
	}
}

// An API server that cannot answer is different from one that answers "taken",
// and a caller that treated the two alike would start a second run on an outage.
func TestClaimReportsAnUndecidableCreate(t *testing.T) {
	l, client := newTestLeases(t, newFixedClock())
	client.PrependReactor("create", "leases",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(errClaimTakenOver)
		})

	if _, ok, err := l.Acquire(context.Background(), testClaim); err == nil || ok {
		t.Errorf("Acquire against a failing API server = (ok %v, err %v), want an error", ok, err)
	}
}

func TestClaimCloseDeletesTheObjectAndIsIdempotent(t *testing.T) {
	l, client := newTestLeases(t, newFixedClock())

	lease, ok, _ := l.Acquire(context.Background(), testClaim)
	if !ok {
		t.Fatal("Acquire was refused")
	}
	for i := range 2 {
		if err := lease.Close(); err != nil {
			t.Fatalf("Close() #%d error = %v, want nil", i+1, err)
		}
	}

	_, err := client.CoordinationV1().Leases(testNamespace).
		Get(context.Background(), claimName(testDeploy, testClaim), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("the claim's object still exists after Close: err = %v, want NotFound", err)
	}
	select {
	case <-lease.Done():
	default:
		t.Error("Done() is open after Close, want closed")
	}
}

// The holder has to hear that it lost the claim, because everything it is doing
// under that claim rests on still owning it.
func TestClaimDoneClosesWhenTheObjectNamesSomeoneElse(t *testing.T) {
	l, client := newTestLeases(t, newFixedClock())

	// The shortest TTL any module honours, so the first renewal — and with it the
	// discovery that the object names somebody else — is due in a third of a
	// second rather than at the default's ten.
	lease, ok, _ := l.Acquire(context.Background(), testClaim, core.WithLeaseTTL(core.MinLeaseTTL))
	if !ok {
		t.Fatal("Acquire was refused")
	}
	defer func() { _ = lease.Close() }()

	record := storedClaim(t, client)
	other := "someone-else"
	record.Spec.HolderIdentity = &other
	if _, err := client.CoordinationV1().Leases(testNamespace).
		Update(context.Background(), record, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("rewriting the holder: %v", err)
	}

	select {
	case <-lease.Done():
	case <-time.After(2 * time.Second):
		t.Error("Done() stayed open after the object named another holder, want closed")
	}
}

func TestClaimNamesAreValidDeterministicAndDistinct(t *testing.T) {
	tests := []struct {
		name       string
		deployment string
		claim      string
	}{
		{name: "plain", deployment: "dep-1", claim: "orders"},
		{name: "punctuation in the claim", deployment: "dep-1", claim: "user@example.com/thread 7"},
		{name: "punctuation in the deployment", deployment: "Dep_1.X", claim: "orders"},
	}
	seen := map[string]string{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := claimName(tc.deployment, tc.claim)
			if got != claimName(tc.deployment, tc.claim) {
				t.Error("claimName is not deterministic")
			}
			if len(got) > maxObjectName {
				t.Errorf("claimName length = %d, want <= %d", len(got), maxObjectName)
			}
			if strings.ToLower(got) != got || strings.ContainsAny(got, "_./@ ") {
				t.Errorf("claimName = %q, want a DNS-1123 name", got)
			}
			if prev, ok := seen[got]; ok {
				t.Errorf("claimName collided with %q", prev)
			}
			seen[got] = tc.name
		})
	}
}

// The two users of coordination Leases must never derive the same object name, or
// an election and a claim on the same text would read each other as competitors.
func TestClaimNamesNeverCollideWithLeaderElectionNames(t *testing.T) {
	for _, key := range []string{"orders", "cron/nightly", ""} {
		if claim, election := claimName(testDeploy, key), leaseName(testDeploy, key); claim == election {
			t.Errorf("claim and election objects collide on %q: both are %q", key, claim)
		}
	}
}
