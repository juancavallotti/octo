package core

import (
	"sync"

	"github.com/juancavallotti/eip-go/types"
)

// FlowEventHandler reacts to a published flow event. Handlers must not block;
// long-running work should be dispatched to the handler's own goroutine.
type FlowEventHandler func(event types.FlowEvent)

// EventBus is a process-wide, fan-out pub/sub dedicated to flow events. Every
// subscriber receives every event. It is safe for concurrent Publish and
// Subscribe.
type EventBus struct {
	mu          sync.RWMutex
	subscribers []FlowEventHandler
}

// NewEventBus returns an empty event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers a handler to receive all future events.
func (b *EventBus) Subscribe(handler FlowEventHandler) {
	if handler == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, handler)
}

// Publish delivers event to every current subscriber. Handlers are invoked
// synchronously, so by contract they must return quickly.
func (b *EventBus) Publish(event types.FlowEvent) {
	b.mu.RLock()
	handlers := make([]FlowEventHandler, len(b.subscribers))
	copy(handlers, b.subscribers)
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
