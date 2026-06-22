package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// errFlowGone reports a send to a flow whose input channel was closed mid-send
// (only reachable during shutdown, when a flow tears down while another is still
// calling it).
var errFlowGone = errors.New("target flow is shutting down")

// callResult is the outcome the event-bus handler delivers to a parked caller.
type callResult struct {
	msg *types.Message
	err error
}

// flowRegistry maps flow names to their input channels and resolves the result of
// a direct invocation through the flow-event bus. It implements core.FlowCaller.
//
// Registration is driven by the implicit source: a flow without an external
// source registers its input channel under the flow name, and callers (the CLI
// and the flow-ref block) look it up to push messages in. Result correlation
// mirrors the HTTP connector: a single bus subscription resolves terminal events
// to parked callers keyed by the message EventID.
type flowRegistry struct {
	mu          sync.RWMutex
	chans       map[string]chan<- *types.Message
	pending     map[string]chan callResult
	unsubscribe func()
}

// newFlowRegistry returns a registry subscribed to bus for result correlation.
func newFlowRegistry(bus *core.EventBus) *flowRegistry {
	r := &flowRegistry{
		chans:       make(map[string]chan<- *types.Message),
		pending:     make(map[string]chan callResult),
		unsubscribe: func() {},
	}
	if bus != nil {
		r.unsubscribe = bus.Subscribe(r.onFlowEvent)
	}
	return r
}

// close releases the registry's bus subscription. Safe to call once.
func (r *flowRegistry) close() {
	r.unsubscribe()
}

// register binds name to its input channel, rejecting a duplicate so a name
// resolves unambiguously.
func (r *flowRegistry) register(name string, ch chan<- *types.Message) error {
	if name == "" {
		return errors.New("a callable flow requires a name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.chans[name]; dup {
		return fmt.Errorf("flow %q is already registered", name)
	}
	r.chans[name] = ch
	return nil
}

// deregister removes name's binding; safe to call when absent.
func (r *flowRegistry) deregister(name string) {
	r.mu.Lock()
	delete(r.chans, name)
	r.mu.Unlock()
}

// lookup returns the input channel registered for name.
func (r *flowRegistry) lookup(name string) (chan<- *types.Message, bool) {
	r.mu.RLock()
	ch, ok := r.chans[name]
	r.mu.RUnlock()
	return ch, ok
}

// Call sends msg to the named flow and waits for its terminal outcome.
func (r *flowRegistry) Call(ctx context.Context, name string, msg *types.Message) (*types.Message, error) {
	reply := r.track(msg.EventID)
	defer r.forget(msg.EventID)

	if err := r.Send(ctx, name, msg); err != nil {
		return nil, err
	}

	select {
	case res := <-reply:
		return res.msg, res.err
	case <-ctx.Done():
		return nil, fmt.Errorf("call flow %q: %w", name, ctx.Err())
	}
}

// Send delivers msg to the named flow without awaiting a result.
func (r *flowRegistry) Send(ctx context.Context, name string, msg *types.Message) (err error) {
	ch, ok := r.lookup(name)
	if !ok {
		return fmt.Errorf("flow %q is not registered", name)
	}
	// A flow can tear down concurrently during shutdown, closing its channel
	// after we read it; turn the resulting panic into an error.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("send to flow %q: %w", name, errFlowGone)
		}
	}()
	select {
	case ch <- msg:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("send to flow %q: %w", name, ctx.Err())
	}
}

// track registers a buffered reply channel under eventID. The buffer of one lets
// onFlowEvent deliver without ever blocking the flow worker.
func (r *flowRegistry) track(eventID string) chan callResult {
	ch := make(chan callResult, 1)
	r.mu.Lock()
	r.pending[eventID] = ch
	r.mu.Unlock()
	return ch
}

// forget removes the pending entry for eventID; safe to call more than once.
func (r *flowRegistry) forget(eventID string) {
	r.mu.Lock()
	delete(r.pending, eventID)
	r.mu.Unlock()
}

// onFlowEvent delivers a terminal flow event to the matching parked caller. It
// runs synchronously on the flow worker, so it never blocks: the reply channel is
// buffered and the send is non-blocking. Started events carry no outcome.
func (r *flowRegistry) onFlowEvent(ev types.FlowEvent) {
	if ev.Kind == types.FlowEventStarted {
		return
	}
	r.mu.Lock()
	ch, ok := r.pending[ev.EventID]
	r.mu.Unlock()
	if !ok {
		return
	}

	res := callResult{}
	switch ev.Kind {
	case types.FlowEventCompleted:
		res.msg = ev.Result
	case types.FlowEventFailed:
		res.err = ev.Err
	case types.FlowEventDropped:
		// result stays nil: a dropped flow has no output.
	}
	select {
	case ch <- res:
	default:
	}
}
