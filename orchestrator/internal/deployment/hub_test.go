package deployment

import "testing"

func TestHubNotifyReachesSubscriber(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("int-1")
	defer cancel()

	h.Notify("int-1")
	select {
	case <-ch:
	default:
		t.Fatal("expected a tick after Notify")
	}
}

func TestHubNotifyOnlyTargetsIntegration(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("int-1")
	defer cancel()

	h.Notify("int-2") // different integration
	select {
	case <-ch:
		t.Fatal("did not expect a tick for a different integration")
	default:
	}
}

func TestHubCoalescesBursts(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("int-1")
	defer cancel()

	for i := 0; i < 5; i++ {
		h.Notify("int-1")
	}
	// Buffer of one: a burst collapses to a single pending tick.
	if len(ch) != 1 {
		t.Fatalf("buffered ticks = %d, want 1 (coalesced)", len(ch))
	}
	<-ch
	select {
	case <-ch:
		t.Fatal("expected only one coalesced tick")
	default:
	}
}

func TestHubCancelUnsubscribes(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("int-1")
	cancel()

	// After cancel the channel is closed and Notify must not panic.
	h.Notify("int-1")
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
	// Cancel is idempotent.
	cancel()
}
