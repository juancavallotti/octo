// The standalone module's fail-fast leases: a map of claimed names under one
// mutex.
//
// This is not a reduced stand-in for the cluster implementation. Exclusivity is
// only ever asked of the processes that could compete for a name, and in this
// module there is exactly one of them — so a mutex is the complete and exact
// answer, in the way that an in-process channel is a complete answer for queues.
package standalone

import (
	"context"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// renewDivisor sets the renewal interval as a fraction of the lease TTL. Three
// renewals per TTL means two may be missed — to a scheduler that stalled the
// goroutine, or a caller holding the mutex — before the claim is at risk.
const renewDivisor = 3

// leases hands out claims on names, and remembers who holds what.
type leases struct {
	mu   sync.Mutex
	held map[string]*heldLease
	// now is the clock deadlines are measured against, injected so a test can
	// age a claim without waiting for one.
	now func() time.Time
}

func newLeases(now func() time.Time) *leases {
	return &leases{held: make(map[string]*heldLease), now: now}
}

// Acquire claims name, or reports that somebody else holds it. It never waits:
// the whole decision is taken under one mutex and returns.
//
// A claim whose deadline has passed is displaced rather than respected. In one
// process that means a holder which panicked past its Close, or one whose renewal
// goroutine died with it — the alternative is a name held until the process
// restarts.
//
//nolint:ireturn // satisfies core.Leases
func (l *leases) Acquire(_ context.Context, name string, opts ...core.LeaseOption) (core.Lease, bool, error) {
	cfg := core.NewLeaseConfig(opts...)

	l.mu.Lock()
	defer l.mu.Unlock()

	if current, ok := l.held[name]; ok {
		if l.now().Before(current.deadline) {
			return nil, false, nil
		}
		current.loseLocked()
	}

	lease := &heldLease{
		owner:    l,
		name:     name,
		deadline: l.now().Add(cfg.TTL),
		done:     make(chan struct{}),
	}
	l.held[name] = lease
	go lease.renew(cfg.TTL / renewDivisor)
	return lease, true, nil
}

// closeAll releases every outstanding claim, so a module shutting down does not
// leave renewal goroutines behind it.
func (l *leases) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for name, lease := range l.held {
		lease.loseLocked()
		delete(l.held, name)
	}
}

// heldLease is one claim. Its mutable state is guarded by its owner's mutex, so
// that taking a name and losing one are decided under the same lock — a claim
// handed out while its predecessor was halfway through expiring would be two
// holders of one name.
type heldLease struct {
	owner    *leases
	name     string
	deadline time.Time
	done     chan struct{}
	lost     bool
}

// Done is closed when the claim is gone, whether it was released or expired.
func (h *heldLease) Done() <-chan struct{} { return h.done }

// Close releases the claim, and is idempotent.
//
// It removes the entry only when it is still this lease's: a claim that already
// expired may have been taken by somebody else, and closing it must not evict the
// successor.
func (h *heldLease) Close() error {
	h.owner.mu.Lock()
	defer h.owner.mu.Unlock()
	if h.owner.held[h.name] == h {
		delete(h.owner.held, h.name)
	}
	h.loseLocked()
	return nil
}

// loseLocked marks the claim gone and wakes anything gating on it. The caller
// holds the owner's mutex.
func (h *heldLease) loseLocked() {
	if h.lost {
		return
	}
	h.lost = true
	close(h.done)
}

// renew pushes the deadline out for as long as the claim is held, so a holder
// that is merely slow is not displaced by a challenger. It exits when the claim
// goes, which is what stops the goroutine on Close.
//
// The interval is positive because core.NewLeaseConfig floors the TTL at
// core.MinLeaseTTL, which a ticker requires — it panics on anything else.
func (h *heldLease) renew(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			if !h.extend(every * renewDivisor) {
				return
			}
		}
	}
}

// extend moves the deadline on, reporting false once the claim is gone.
func (h *heldLease) extend(ttl time.Duration) bool {
	h.owner.mu.Lock()
	defer h.owner.mu.Unlock()
	if h.lost {
		return false
	}
	h.deadline = h.owner.now().Add(ttl)
	return true
}
