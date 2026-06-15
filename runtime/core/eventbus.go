package core

import (
	"sync"

	"github.com/juancavallotti/eip-go/types"
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
	mu          sync.RWMutex
	nextID      uint64
	subscribers []subscription
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
	b.subscribers = append(b.subscribers, subscription{id: id, handler: handler})
	b.mu.Unlock()
	return func() { b.unsubscribe(id) }
}

// unsubscribe removes the subscription with the given id, if present.
func (b *EventBus) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.subscribers {
		if b.subscribers[i].id == id {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

// Publish delivers event to every current subscriber. Handlers are invoked
// synchronously, so by contract they must return quickly.
func (b *EventBus) Publish(event types.FlowEvent) {
	b.mu.RLock()
	handlers := make([]FlowEventHandler, len(b.subscribers))
	for i := range b.subscribers {
		handlers[i] = b.subscribers[i].handler
	}
	b.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}

var defaultEventBus = NewEventBus()

// DefaultEventBus returns the process-wide flow-event bus.
func DefaultEventBus() *EventBus {
	return defaultEventBus
}
