package ingest

import (
	"context"
	"sync"
	"time"
)

const (
	// unknownTTL is how long an unresolved deployment is remembered as unresolved.
	//
	// It exists because the two reasons a lookup misses have opposite needs. A
	// burst of records from a deployment that no longer exists must not become a
	// burst of queries; but a deployment created moments ago must start resolving
	// shortly after, not at the next restart. A short negative memory serves both.
	unknownTTL = 30 * time.Second

	// lookupTimeout bounds the shared lookup. It replaces the caller's deadline
	// rather than inheriting it, so one caller giving up cannot decide the answer
	// for everyone waiting behind it — see Resolve.
	lookupTimeout = 5 * time.Second

	// resolverCapacity bounds the cache. Entries are keyed by deployment, so the
	// working set is the number of deployments a cluster runs — a few hundred at
	// the outside. The cap is a backstop against a flood of ids that resolve to
	// nothing, and it clears wholesale rather than evicting least-recently-used:
	// rebuilding a few hundred entries costs a few hundred indexed lookups, which
	// is far less than an eviction policy costs to carry.
	resolverCapacity = 4096
)

// deployments is the lookup an IntegrationResolver needs. Declared here, where it
// is consumed, so the caching can be tested without a database.
type deployments interface {
	IntegrationOf(ctx context.Context, deploymentID string) (string, bool, error)
}

// IntegrationResolver maps a deployment to the integration that owns it, once.
//
// A deployment never changes hands, so a resolved answer is cached for the life
// of the process rather than for a while. That is the whole reason this is worth
// caching at all: at trace volumes, resolving per record would put an indexed
// query in front of every block invocation of every message.
type IntegrationResolver struct {
	lookup deployments

	mu      sync.Mutex
	entries map[string]*resolution

	// now is the clock, replaced in tests so expiry can be exercised without
	// waiting for it.
	now func() time.Time
}

// resolution is one lookup, in flight or complete.
//
// done is what makes concurrent callers for the same deployment share a single
// query: the first to arrive installs the entry and performs the lookup, and
// everyone behind it waits on the channel rather than issuing a query of their
// own. Without it a cold start under load asks the same question once per record.
type resolution struct {
	done chan struct{}

	integrationID string
	found         bool
	err           error

	// expires is the zero time while the lookup is in flight and for a resolved
	// answer, which never expires. Only a miss is given a deadline.
	expires time.Time
}

// NewIntegrationResolver returns a resolver over lookup.
func NewIntegrationResolver(lookup deployments) *IntegrationResolver {
	return &IntegrationResolver{
		lookup:  lookup,
		entries: make(map[string]*resolution),
		now:     time.Now,
	}
}

// Resolve returns the integration owning a deployment, reporting false when there
// is none to be found. An error means the lookup itself failed and the answer is
// unknown, which is not the same as there being no owner.
func (r *IntegrationResolver) Resolve(ctx context.Context, deploymentID string) (string, bool, error) {
	entry, mine := r.entryFor(deploymentID)
	if !mine {
		// Someone else is asking, or has already asked. Wait for their answer.
		select {
		case <-entry.done:
			return entry.integrationID, entry.found, entry.err
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}

	// Detached from the caller's context on purpose. Everyone waiting on this
	// entry takes its answer, so running the query under the first caller's
	// deadline would let that one caller's cancellation resolve the deployment as
	// unknown for every record behind it. The lookup gets a deadline of its own
	// instead; each waiter still honours its own ctx while waiting.
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lookupTimeout)
	entry.integrationID, entry.found, entry.err = r.lookup.IntegrationOf(lookupCtx, deploymentID)
	cancel()

	r.settle(deploymentID, entry)
	close(entry.done)
	return entry.integrationID, entry.found, entry.err
}

// entryFor returns the entry for a deployment and whether the caller owns
// performing the lookup for it.
func (r *IntegrationResolver) entryFor(deploymentID string) (*resolution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, ok := r.entries[deploymentID]; ok && !r.expired(entry) {
		return entry, false
	}
	if len(r.entries) >= resolverCapacity {
		r.entries = make(map[string]*resolution, resolverCapacity)
	}
	entry := &resolution{done: make(chan struct{})}
	r.entries[deploymentID] = entry
	return entry, true
}

// settle records how long a completed lookup may be trusted.
//
// A hit is kept indefinitely, since the relation it recorded cannot change. A
// miss is kept briefly. A failure is dropped outright rather than cached: it says
// nothing about the deployment, and remembering it would keep answering with a
// database outage after the database came back.
func (r *IntegrationResolver) settle(deploymentID string, entry *resolution) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch {
	case entry.err != nil:
		// Only remove the entry if it is still ours; a capacity clear may have
		// replaced the whole map underneath.
		if current, ok := r.entries[deploymentID]; ok && current == entry {
			delete(r.entries, deploymentID)
		}
	case !entry.found:
		entry.expires = r.now().Add(unknownTTL)
	}
}

// expired reports whether an entry has outlived its deadline. An entry with no
// deadline — in flight, or resolved — never expires.
func (r *IntegrationResolver) expired(entry *resolution) bool {
	return !entry.expires.IsZero() && r.now().After(entry.expires)
}
