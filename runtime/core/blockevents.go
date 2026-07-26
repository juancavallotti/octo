package core

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/juancavallotti/octo/runtime/types"
)

// asyncBuffer is the depth of the queue feeding the async listeners. It matches
// the per-subscription buffer the topics service uses, for the same reason: the
// producer is the flow itself, and it must never wait on an observer.
const asyncBuffer = 1024

// SyncBlockListener observes a block invocation on the flow's own goroutine,
// before the block runs and again after it returns. The flow waits for it, so a
// listener that blocks stalls the flow — which is what makes a debugger possible.
//
// It receives the flow's context, so a listener that parks can honour
// cancellation. It may read the event's message and may copy it (Clone, Scoped,
// Reported): nothing else is touching the message while this runs.
//
// One flow runs on many goroutines — a worker pool per flow, and fork branches on
// a shared pool — so a listener must be safe for concurrent use. A panic in a sync
// listener propagates into the flow, deliberately: it is running as part of the
// flow, and swallowing it would hide the bug.
//
// A sync listener observes; it cannot skip or abort the block.
type SyncBlockListener func(ctx context.Context, event types.BlockEvent)

// AsyncBlockListener observes a block invocation after the fact, on the
// dispatcher's own goroutine. All async listeners share that one goroutine and are
// called in registration order, one event at a time.
//
// Delivery is at-most-once: when the queue is full the event is dropped rather
// than made to wait, so a slow listener costs telemetry rather than throughput.
//
// It must not dereference event.Message.Body or event.Message.Variables — see the
// contract on types.BlockEvent.Message. Everything else on the event is a value
// copied on the flow's goroutine and is safe.
type AsyncBlockListener func(event types.BlockEvent)

// listeners is one immutable snapshot of the registered listeners. It is replaced
// wholesale on registration so the emit path can read it without a lock.
type listeners struct {
	sync  []SyncBlockListener
	async []AsyncBlockListener
}

// BlockEvents dispatches the pre- and post-invoke events the flow engine emits
// around every block. It is the seam behind flow debugging (sync) and per-block
// telemetry (async).
//
// It is the block-level counterpart to EventBus, which carries per-message flow
// events. The two differ where the cost does: EventBus fans out under a read lock
// on the terminal event of a whole flow, while this sits on the per-block hot path,
// so its registered set is read through an atomic and its slow consumers are
// pushed onto a queue instead of being waited for.
//
// The zero-listener case costs one atomic load: see Active.
type BlockEvents struct {
	// set is the current listeners, replaced (never mutated) on registration.
	set atomic.Pointer[listeners]
	// mu serializes registration. The emit path never takes it.
	mu sync.Mutex

	// ch feeds the async listeners. It is allocated once, in New, so Emit never
	// races a lazily-created channel.
	ch chan types.BlockEvent
	// startPump spawns the drain goroutine on the first async registration, so a
	// dispatcher nobody listens to asynchronously never starts one.
	startPump sync.Once
	stopPump  sync.Once
	done      chan struct{}
	wg        sync.WaitGroup

	dropped  atomic.Int64
	warnOnce sync.Once
}

// NewBlockEvents returns a dispatcher with no listeners.
func NewBlockEvents() *BlockEvents {
	e := &BlockEvents{
		ch:   make(chan types.BlockEvent, asyncBuffer),
		done: make(chan struct{}),
	}
	e.set.Store(&listeners{})
	return e
}

var defaultBlockEvents = NewBlockEvents()

// DefaultBlockEvents returns the process-wide block-event dispatcher, the one a
// Service uses unless an embedder wires its own.
func DefaultBlockEvents() *BlockEvents {
	return defaultBlockEvents
}

// AddSync registers a listener called inline, on the flow's goroutine. It takes
// effect on the next block invoked.
func (e *BlockEvents) AddSync(listener SyncBlockListener) {
	if listener == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	current := e.set.Load()
	next := &listeners{
		sync:  append(append([]SyncBlockListener(nil), current.sync...), listener),
		async: current.async,
	}
	e.set.Store(next)
}

// AddAsync registers a listener called off the flow's goroutine, and starts the
// drain goroutine if this is the first one. It takes effect on the next block
// invoked.
func (e *BlockEvents) AddAsync(listener AsyncBlockListener) {
	if listener == nil {
		return
	}
	e.mu.Lock()
	current := e.set.Load()
	next := &listeners{
		sync:  current.sync,
		async: append(append([]AsyncBlockListener(nil), current.async...), listener),
	}
	e.set.Store(next)
	e.mu.Unlock()

	e.startPump.Do(func() {
		e.wg.Add(1)
		go e.pump()
	})
}

// Active reports whether anything is listening. The engine calls it once per block,
// so it is one atomic load and two length checks — the whole cost of the feature
// when nobody has registered.
func (e *BlockEvents) Active() bool {
	set := e.set.Load()
	return len(set.sync) > 0 || len(set.async) > 0
}

// Emit delivers the event: every sync listener inline, in registration order, then
// the async queue. It runs on the flow's goroutine, so it does exactly as much work
// as it must and never blocks on the queue.
func (e *BlockEvents) Emit(ctx context.Context, event types.BlockEvent) {
	set := e.set.Load()
	for _, listener := range set.sync {
		listener(ctx, event)
	}
	if len(set.async) == 0 {
		return
	}

	select {
	case e.ch <- event:
	default:
		// At-most-once: a listener that fell behind loses events rather than
		// slowing the flow down to its speed. Warn once — a saturated queue would
		// otherwise turn the log into the next bottleneck — and keep the count.
		e.dropped.Add(1)
		e.warnOnce.Do(func() {
			slog.Warn("block events: async listeners fell behind, dropping events",
				"flow", event.Flow, "path", event.Path, "buffer", asyncBuffer)
		})
	}
}

// Dropped returns how many events were discarded because the async queue was full.
func (e *BlockEvents) Dropped() int64 { return e.dropped.Load() }

// Stop drains what is queued, calls the async listeners for it, and stops the drain
// goroutine. It is idempotent.
//
// A Service does not call it: the default dispatcher is process-wide and outlives
// any one generation of a hot-reloading runtime, the same way DefaultEventBus does.
// It exists for tests that need delivery to have happened before they assert, and
// for an embedder that owns its own dispatcher.
//
// The queue is never closed, only abandoned: closing it would race an in-flight
// Emit into a panic, and guarding that would put a lock on the hot path. Events
// emitted after Stop are buffered and never delivered.
func (e *BlockEvents) Stop() {
	e.stopPump.Do(func() {
		close(e.done)
		e.wg.Wait()
		if dropped := e.dropped.Load(); dropped > 0 {
			slog.Warn("block events: dropped events", "count", dropped)
		}
	})
}

// pump is the single goroutine behind the async listeners. Calling them one event
// at a time is the contract: a listener may keep unsynchronized state.
func (e *BlockEvents) pump() {
	defer e.wg.Done()
	for {
		select {
		case event := <-e.ch:
			e.deliver(event)
		case <-e.done:
			// Drain what is already queued before leaving, so a test that stops the
			// dispatcher sees everything it emitted.
			for {
				select {
				case event := <-e.ch:
					e.deliver(event)
				default:
					return
				}
			}
		}
	}
}

// deliver calls each async listener, containing a panic so one bad listener costs
// its own event rather than every future event for the whole process.
func (e *BlockEvents) deliver(event types.BlockEvent) {
	for _, listener := range e.set.Load().async {
		e.call(listener, event)
	}
}

func (e *BlockEvents) call(listener AsyncBlockListener, event types.BlockEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("block events: async listener panicked",
				"flow", event.Flow, "path", event.Path, "panic", r)
		}
	}()
	listener(event)
}
