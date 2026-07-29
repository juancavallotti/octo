package core

import (
	"sync"
	"sync/atomic"

	"github.com/juancavallotti/octo/runtime/types"
)

// FlowEventHandler reacts to a published flow event. Handlers must not block;
// long-running work should be dispatched to the handler's own goroutine.
type FlowEventHandler func(event types.FlowEvent)

// subscription pairs a handler with the id used to remove it.
type subscription struct {
	id      uint64
	handler FlowEventHandler
}

// EventBus is a process-wide, fan-out pub/sub dedicated to flow events. Every
// subscriber receives every event. It is safe for concurrent Publish and
// Subscribe.
type EventBus struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers atomic.Pointer[[]subscription]
}

// NewEventBus returns an empty event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers a handler to receive all future events and returns a
// function that removes it. Callers that never unsubscribe (process-lifetime
// handlers) may ignore the return value; long-lived embedders that rebuild
// (e.g. a hot-reloading service) should call it on teardown to avoid leaking
// stale handlers.
func (b *EventBus) Subscribe(handler FlowEventHandler) (unsubscribe func()) {
	if handler == nil {
		return func() {}
	}
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	cur := b.snapshot()
	next := append(make([]subscription, 0, len(cur)+1), cur...)
	next = append(next, subscription{id: id, handler: handler})
	b.subscribers.Store(&next)
	b.mu.Unlock()
	return func() { b.unsubscribe(id) }
}

// unsubscribe removes the subscription with the given id, if present.
func (b *EventBus) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur := b.snapshot()
	for i := range cur {
		if cur[i].id == id {
			next := append([]subscription{}, cur[:i]...)
			next = append(next, cur[i+1:]...)
			b.subscribers.Store(&next)
			return
		}
	}
}

// snapshot returns the current subscriber slice, or nil if none has been
// stored yet. Callers under mu use it as the base to copy from; Publish uses
// it directly since it never mutates what it reads.
func (b *EventBus) snapshot() []subscription {
	if p := b.subscribers.Load(); p != nil {
		return *p
	}
	return nil
}

// Publish delivers event to every current subscriber. Handlers are invoked
// synchronously, so by contract they must return quickly. A snapshot taken
// before an unsubscribe may still call the removed handler once — this was
// already true of the previous RWMutex-based implementation, so it is not a
// new behavioral risk, just documented here.
func (b *EventBus) Publish(event types.FlowEvent) {
	p := b.subscribers.Load()
	if p == nil {
		return
	}
	for _, s := range *p {
		s.handler(event)
	}
}

var defaultEventBus = NewEventBus()

// DefaultEventBus returns the process-wide flow-event bus.
func DefaultEventBus() *EventBus {
	return defaultEventBus
}
