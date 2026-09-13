// A claim on a name that expires, and that a caller can fail to take.
//
// This is the primitive for "one of us does this, and the rest do something
// else": a conversation owned by one replica, a migration run once, a resource
// touched by a single writer.
package core

import (
	"context"
	"sync"
	"time"
)

// DefaultLeaseTTL is how long a claim outlives its holder's last renewal — the
// window in which a name stays claimed by a process that has stopped answering.
// Thirty seconds is comfortably longer than any single renewal round trip and
// short enough that a dead holder is not remembered for long.
const DefaultLeaseTTL = 30 * time.Second

// MinLeaseTTL is the shortest claim an implementation honours. A TTL is resolved
// to whole seconds, so anything shorter would round to zero and read as expired
// the instant it was written, handing the name straight to the next caller. It
// also keeps the renewal interval, a third of the TTL, positive.
const MinLeaseTTL = time.Second

// Lease is a claim on a name, held until it is released or its holder stops
// renewing.
//
// The holder renews in the background for as long as it holds the lease, so a
// caller does not schedule anything. Done is how it learns it has lost the claim:
// unlike a lock, a lease can be taken away — by a network it could not reach, or
// by an expiry it did not renew in time — so work done under one has to be able
// to hear that.
type Lease interface {
	// Done is closed when the claim is no longer held: released by Close, or lost
	// because a renewal did not land. A holder gates its work on it.
	Done() <-chan struct{}
	// Close releases the claim. It is idempotent, so a deferred Close beside an
	// explicit one is not an error.
	Close() error
}

// Leases hands out exclusive, expiring claims on a name, and never waits.
//
// A name somebody else holds comes back ok=false immediately, so a caller can do
// something else with the message it is holding instead of queueing behind a run
// it cannot see. That immediacy is the whole feature, and it is what separates a
// claim from an election: Acquire answers now and answers definitively, where an
// election converges on an answer later. An operation that blocks until a name is
// free is a different tool, and not one that can be used on the request path.
//
// Every RuntimeServices exposes one, so there is nothing for a caller to
// type-assert.
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
// <= 0 is ignored (the default applies), and anything below MinLeaseTTL is raised
// to it.
func WithLeaseTTL(d time.Duration) LeaseOption {
	return func(c *LeaseConfig) { c.TTL = d }
}

// NewLeaseConfig resolves opts into a LeaseConfig, applying DefaultLeaseTTL when no
// positive value was set and MinLeaseTTL when the value is too short to honour.
// Modules call it to read the effective settings, and may rely on the result being
// at least MinLeaseTTL.
//
// The two clamps mean different things and so are not one branch: a
// non-positive TTL is an option that was never really set, and takes the default;
// a short one was set deliberately and takes the smallest thing that works.
func NewLeaseConfig(opts ...LeaseOption) LeaseConfig {
	cfg := LeaseConfig{TTL: DefaultLeaseTTL}
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case cfg.TTL <= 0:
		cfg.TTL = DefaultLeaseTTL
	case cfg.TTL < MinLeaseTTL:
		cfg.TTL = MinLeaseTTL
	}
	return cfg
}

// NoopLeases returns leases that grant every claim. It is the fallback the no-op
// services expose for a context that was never wired with real ones, where
// refusing would be the more surprising answer.
//
// It is not an implementation for a single process: runs within one process still
// compete for the same name, and granting every claim would let two of them
// believe they own it.
//
//nolint:ireturn // returns the Leases interface intentionally
func NoopLeases() Leases { return noopLeases{} }

// noopLeases grants every claim and holds nothing.
type noopLeases struct{}

//nolint:ireturn // satisfies the Leases interface
func (noopLeases) Acquire(context.Context, string, ...LeaseOption) (Lease, bool, error) {
	return &grantedLease{done: make(chan struct{})}, true, nil
}

// grantedLease is a claim nothing can take away, held until it is released.
//
// It carries a real channel rather than a nil one: nothing will ever close it on
// this lease's behalf, but Close must, or a holder gating on Done would block
// forever on a claim it released itself.
type grantedLease struct {
	done chan struct{}
	once sync.Once
}

// Done is closed by Close, and by nothing else.
func (l *grantedLease) Done() <-chan struct{} { return l.done }

func (l *grantedLease) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}
