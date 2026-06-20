package deployment

import "sync"

// Hub is a per-integration pub-sub used to push deployment status changes to SSE
// subscribers. Subscribers receive a coalesced "something changed" tick (not the
// payload); they then re-read the current list. Safe for concurrent use.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{} // integrationID -> set of tick channels
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan struct{}]struct{})}
}

// Subscribe registers interest in an integration's changes and returns a tick
// channel plus a cancel func that unsubscribes and closes the channel. The
// channel is buffered by one and ticks are coalesced, so a slow consumer never
// blocks the notifier and never sees a backlog.
func (h *Hub) Subscribe(integrationID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[integrationID] == nil {
		h.subs[integrationID] = make(map[chan struct{}]struct{})
	}
	h.subs[integrationID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[integrationID], ch)
			if len(h.subs[integrationID]) == 0 {
				delete(h.subs, integrationID)
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Notify wakes every subscriber of integrationID. The non-blocking send coalesces
// bursts: if a tick is already pending on a channel, further ticks are dropped.
// Holding the lock while sending guarantees we never send on a closed channel,
// since cancel removes a channel under the same lock before closing it.
func (h *Hub) Notify(integrationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[integrationID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
