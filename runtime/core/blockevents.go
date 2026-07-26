package core

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/juancavallotti/octo/runtime/types"
)

// SyncBlockListener observes a block invocation on the flow's own goroutine,
// before the block runs and again after it returns. The flow waits for it, so a
// listener that blocks stalls the flow — which is what makes a debugger possible.
//
// It receives the flow's context, so a listener that parks can honour
// cancellation. It may read the event's message and may copy it (Clone, Scoped,
// Reported): nothing else is touching the message while this runs.
//
// One flow runs on many goroutines — a worker pool per flow, and fork branches on
// a shared pool — so a listener must be safe for concurrent use, and must be cheap:
// its cost is paid once per watched block per message, on the hot path. A panic
// propagates into the flow, deliberately: it is running as part of the flow, and
// swallowing it would hide the bug.
//
// A sync listener observes; it cannot skip or abort the block.
//
// Inline is the only delivery this dispatcher offers. A queued, off-goroutine
// variant used to exist and was removed: the producers are every flow worker in
// the process and the consumer was a single goroutine, so no buffer size makes the
// consumer keep up with sustained load — it only sets how long saturation takes to
// start dropping. Worse, tail-dropping a full queue is not sampling, it is
// "discard during bursts", which biases telemetry against exactly the traffic
// worth measuring. A listener that genuinely must do slow or blocking work should
// Clone what it needs here and hand the copy to a worker it owns, so the loss
// policy is its own rather than the runtime's.
type SyncBlockListener func(ctx context.Context, event types.BlockEvent)

// registration pairs a listener with the block paths it asked for.
type registration struct {
	fn SyncBlockListener
	// paths is the set of block paths this listener wants, or nil for every path.
	paths map[string]struct{}
}

// wants reports whether this listener asked for the given block path.
func (r registration) wants(path string) bool {
	if r.paths == nil {
		return true
	}
	_, ok := r.paths[path]
	return ok
}

// listeners is one immutable snapshot of the registered listeners. It is replaced
// wholesale on registration so the emit path can read it without a lock.
type listeners struct {
	registered []registration
	// paths is the union of every filtered listener's set, which is what Observes
	// answers from. It is nil when all is true, because then it would not be read.
	paths map[string]struct{}
	// all is set when any listener registered without a filter, so every block is
	// observed.
	all bool
}

// observes reports whether any registered listener asked for this block path.
func (l *listeners) observes(path string) bool {
	if l.all {
		return true
	}
	if len(l.paths) == 0 {
		return false
	}
	_, ok := l.paths[path]
	return ok
}

// BlockEvents dispatches the pre- and post-invoke events the flow engine emits
// around every block. It is the seam behind flow debugging and per-block telemetry.
//
// It is the block-level counterpart to EventBus, which carries per-message flow
// events. The two differ where the cost does: EventBus fans out on the terminal
// event of a whole flow, while this sits on the per-block hot path — so its
// registered set is read through an atomic, and a listener declares up front which
// block paths it wants, so a block nobody asked about costs one atomic load and a
// map lookup rather than a built event.
//
// The zero-listener case costs one atomic load: see Observes.
type BlockEvents struct {
	// set is the current listeners, replaced (never mutated) on registration.
	set atomic.Pointer[listeners]
	// mu serializes registration. The emit path never takes it.
	mu sync.Mutex
}

// NewBlockEvents returns a dispatcher with no listeners.
func NewBlockEvents() *BlockEvents {
	e := &BlockEvents{}
	e.set.Store(&listeners{})
	return e
}

var defaultBlockEvents = NewBlockEvents()

// DefaultBlockEvents returns the process-wide block-event dispatcher, the one a
// Service uses unless an embedder wires its own.
func DefaultBlockEvents() *BlockEvents {
	return defaultBlockEvents
}

// AddSync registers a listener for every block in every flow. It takes effect on
// the next block invoked.
//
// Watching everything makes the engine build and dispatch an event around every
// block in the process; prefer AddSyncFor when the set of interesting blocks is
// known.
func (e *BlockEvents) AddSync(listener SyncBlockListener) {
	e.add(registration{fn: listener})
}

// AddSyncFor registers a listener that receives events only for the named block
// paths. Other blocks are not dispatched to it, and — unless something else is
// watching them — are not built into events at all.
//
// Passing no paths registers nothing: a listener that asked for no block is not a
// listener that wants every block, and treating it as one is how watching a single
// address turns into watching the process.
func (e *BlockEvents) AddSyncFor(paths []string, listener SyncBlockListener) {
	if len(paths) == 0 {
		return
	}
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		set[path] = struct{}{}
	}
	e.add(registration{fn: listener, paths: set})
}

// add installs one registration, rebuilding the snapshot the emit path reads.
func (e *BlockEvents) add(reg registration) {
	if reg.fn == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	current := e.set.Load()
	next := &listeners{
		registered: append(append([]registration(nil), current.registered...), reg),
		all:        current.all || reg.paths == nil,
	}
	if !next.all {
		next.paths = make(map[string]struct{}, len(current.paths)+len(reg.paths))
		for path := range current.paths {
			next.paths[path] = struct{}{}
		}
		for path := range reg.paths {
			next.paths[path] = struct{}{}
		}
	}
	e.set.Store(next)
}

// Active reports whether anything is listening at all.
func (e *BlockEvents) Active() bool {
	return len(e.set.Load().registered) > 0
}

// Observes reports whether any listener asked about this block path. The engine
// calls it once per block, before it builds anything, so the whole cost of the
// feature is one atomic load when nobody has registered — and one atomic load plus
// a map lookup for a block nobody watches when someone has.
func (e *BlockEvents) Observes(path string) bool {
	return e.set.Load().observes(path)
}

// Emit delivers the event to every listener that asked for its path, inline, in
// registration order. It runs on the flow's goroutine and returns once they have
// all been called, so a listener has observed the block by the time the flow moves
// on — there is no queue, and nothing is dropped.
func (e *BlockEvents) Emit(ctx context.Context, event types.BlockEvent) {
	for _, reg := range e.set.Load().registered {
		if reg.wants(event.Path) {
			reg.fn(ctx, event)
		}
	}
}
