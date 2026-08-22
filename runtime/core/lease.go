// A claim on a name that expires, and that a caller can fail to take.
//
// This is the primitive for "one of us does this, and the rest do something
// else": a conversation owned by one replica, a migration run once, a resource
// touched by a single writer. It is deliberately separate from LeaderElection,
// which answers a different question — see the Leases doc comment.
package core

import (
	"context"
	"time"
)

// DefaultLeaseTTL is how long a claim outlives its holder's last renewal.
//
// It is the window in which a name stays claimed by a process that has stopped
// answering, so it trades prompt recovery against the cost of a holder that is
// merely slow being displaced by a challenger. Thirty seconds is comfortably
// longer than any single renewal round trip and short enough that a dead holder
// is not remembered for long.
const DefaultLeaseTTL = 30 * time.Second

// Lease is a claim on a name, held until it is released or its holder stops
// renewing.
//
// The holder renews in the background for as long as it holds the lease, so a
// caller does not schedule anything. Done is how it learns it has lost the claim,
// and that channel is the difference between a lease and a lock: a lock you hold
// until you let go, whereas a lease can be taken from you — by a network you
// could not reach, or by an expiry you did not renew in time. Work done under one
// has to be able to hear that.
type Lease interface {
	// Done is closed when the claim is no longer held: released by Close, or lost
	// because a renewal did not land. A holder gates its work on it, exactly as a
	// leader gates on Leadership.IsLeader.
	Done() <-chan struct{}
	// Close releases the claim. It is idempotent, so a deferred Close beside an
	// explicit one is not an error.
	Close() error
}

// Leases hands out exclusive, expiring claims on a name, and never waits.
//
// A name somebody else holds comes back ok=false immediately, so a caller can do
// something else with the message it is holding instead of queueing behind a run
// it cannot see. That immediacy is the whole feature: an operation that blocks
// until a name is free is a different tool, and one that cannot be used on the
// request path.
//
// This is not LeaderElection, despite both resting on the same Kubernetes object
// in the k8s module. Acquire answers now and answers definitively. Acquire on a
// LeaderElection starts a background campaign and returns a handle whose IsLeader
// converges later, so a false reading there means "not yet" rather than "somebody
// else has it" — which is exactly the distinction a caller choosing a different
// path needs, and exactly the one an election cannot make. Its fifteen-second
// lease and two-second retry are also sized for one long-lived election per
// process, not one claim per unit of work.
//
// Every module has one. The standalone module's is a map under a mutex, which is
// not a stand-in for the real thing but the complete and exact implementation for
// a single process; the k8s module's is a coordination Lease object. So it is an
// accessor on RuntimeServices rather than an optional side interface — there is
// no module that lacks it, and nothing for a caller to type-assert.
type Leases interface {
	// Acquire claims name for the caller, or reports ok=false because somebody
	// else holds it. It never blocks waiting for a holder to finish. A non-nil
	// error means the claim could not be decided at all, which is different from
	// deciding it is taken.
	//nolint:ireturn // returns the Lease interface the caller holds and closes
	Acquire(ctx context.Context, name string, opts ...LeaseOption) (lease Lease, ok bool, err error)
}

// LeaseOption configures an Acquire call.
type LeaseOption func(*LeaseConfig)

// LeaseConfig is the resolved configuration for a claim. Modules build it from the
// caller's options with NewLeaseConfig.
type LeaseConfig struct {
	// TTL is how long the claim survives without a renewal.
	TTL time.Duration
}

// WithLeaseTTL sets how long a claim outlives its holder's last renewal. A value
// <= 0 is ignored (the default applies).
func WithLeaseTTL(d time.Duration) LeaseOption {
	return func(c *LeaseConfig) { c.TTL = d }
}

// NewLeaseConfig resolves opts into a LeaseConfig, applying DefaultLeaseTTL when no
// positive value was set. Modules call it to read the effective settings.
func NewLeaseConfig(opts ...LeaseOption) LeaseConfig {
	cfg := LeaseConfig{TTL: DefaultLeaseTTL}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultLeaseTTL
	}
	return cfg
}

// NoopLeases returns leases that grant every claim — the single-process reading
// where there is nothing to coordinate, matching NoopLeaderElection. It is the
// fallback the no-op services expose for contexts that were never wired with real
// ones.
//
// Note that this is NOT what the standalone module uses: a single process still
// has runs competing for the same name, and granting every claim there would let
// two of them believe they own it. This exists for a context with no services at
// all, where refusing would be the more surprising answer.
//
//nolint:ireturn // returns the Leases interface intentionally
func NoopLeases() Leases { return noopLeases{} }

// noopLeases grants every claim and holds nothing.
type noopLeases struct{}

//nolint:ireturn // satisfies the Leases interface
func (noopLeases) Acquire(context.Context, string, ...LeaseOption) (Lease, bool, error) {
	return grantedLease{}, true, nil
}

// grantedLease is a claim that is never lost and never has to be released.
type grantedLease struct{}

// Done never closes: nothing can take this claim away.
func (grantedLease) Done() <-chan struct{} { return nil }

func (grantedLease) Close() error { return nil }
