package core

import (
	"context"
	"testing"
	"time"
)

func TestNewLeaseConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []LeaseOption
		want time.Duration
	}{
		{name: "no options", want: DefaultLeaseTTL},
		{name: "a positive ttl is taken", opts: []LeaseOption{WithLeaseTTL(90 * time.Second)}, want: 90 * time.Second},
		// A literal zero would be a claim that expires the moment it is made, which
		// is never what an author meant by leaving it out.
		{name: "zero falls back", opts: []LeaseOption{WithLeaseTTL(0)}, want: DefaultLeaseTTL},
		{name: "negative falls back", opts: []LeaseOption{WithLeaseTTL(-time.Second)}, want: DefaultLeaseTTL},
		// Not the default: a short TTL was asked for deliberately, so it takes the
		// smallest thing every module can honour rather than jumping to thirty
		// seconds. Below a second the k8s module's object would round to zero and
		// read as expired the instant it was written.
		{
			name: "a sub-second ttl is raised to the minimum",
			opts: []LeaseOption{WithLeaseTTL(30 * time.Millisecond)},
			want: MinLeaseTTL,
		},
		{name: "the minimum itself is kept", opts: []LeaseOption{WithLeaseTTL(MinLeaseTTL)}, want: MinLeaseTTL},
		{
			name: "the last option wins",
			opts: []LeaseOption{WithLeaseTTL(time.Minute), WithLeaseTTL(2 * time.Minute)},
			want: 2 * time.Minute,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewLeaseConfig(tc.opts...).TTL; got != tc.want {
				t.Errorf("NewLeaseConfig().TTL = %v, want %v", got, tc.want)
			}
		})
	}
}

// The no-op is the fallback for a context that was never wired with services, so
// it grants rather than refuses: refusing would be the more surprising answer to
// a caller that has no coordination to do.
func TestNoopLeasesGrantEveryClaim(t *testing.T) {
	leases := NoopLeases()

	first, ok, err := leases.Acquire(context.Background(), "orders")
	if err != nil || !ok {
		t.Fatalf("Acquire() = (ok %v, err %v), want granted", ok, err)
	}
	if _, ok, err = leases.Acquire(context.Background(), "orders"); err != nil || !ok {
		t.Errorf("a second Acquire on the same name = (ok %v, err %v), want granted", ok, err)
	}

	select {
	case <-first.Done():
		t.Error("Done() closed on a claim nothing can take away")
	default:
	}

	// Nothing will ever take this claim, but releasing it still has to close Done
	// — a holder gating on it would otherwise block forever on a claim it released
	// itself, and that holder is written against the interface, not this module.
	for i := range 2 {
		if err := first.Close(); err != nil {
			t.Errorf("Close() #%d error = %v, want nil", i+1, err)
		}
	}
	select {
	case <-first.Done():
	default:
		t.Error("Done() is open after Close, want closed")
	}
}

func TestNoopRuntimeServicesExposeLeases(t *testing.T) {
	if NoopRuntimeServices().Leases() == nil {
		t.Error("Leases() is nil on the no-op services, want a usable value")
	}
}
