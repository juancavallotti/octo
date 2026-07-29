package core

import (
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestEventBusFanOut(t *testing.T) {
	bus := NewEventBus()

	var mu sync.Mutex
	got := make(map[int]int)
	for id := 0; id < 3; id++ {
		bus.Subscribe(func(types.FlowEvent) {
			mu.Lock()
			got[id]++
			mu.Unlock()
		})
	}

	bus.Publish(types.FlowEvent{Kind: types.FlowEventCompleted})
	bus.Publish(types.FlowEvent{Kind: types.FlowEventCompleted})

	for id := 0; id < 3; id++ {
		if got[id] != 2 {
			t.Errorf("subscriber %d received %d events, want 2", id, got[id])
		}
	}
}

func TestEventBusConcurrent(t *testing.T) {
	bus := NewEventBus()

	var count int
	var mu sync.Mutex
	bus.Subscribe(func(types.FlowEvent) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	const publishers = 8
	const perPublisher = 50
	var wg sync.WaitGroup
	wg.Add(publishers)
	for p := 0; p < publishers; p++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perPublisher; i++ {
				bus.Publish(types.FlowEvent{Kind: types.FlowEventStarted})
			}
		}()
	}
	wg.Wait()

	if want := publishers * perPublisher; count != want {
		t.Errorf("received %d events, want %d", count, want)
	}
}

func TestEventBusIgnoresNilHandler(t *testing.T) {
	bus := NewEventBus()
	bus.Subscribe(nil)
	bus.Publish(types.FlowEvent{Kind: types.FlowEventStarted}) // must not panic
}

// TestEventBusUnsubscribeDuringPublish confirms that unsubscribing a handler
// from inside another handler's callback does not affect the Publish call
// already in progress: the snapshot that call is iterating was fixed before
// any handler ran, so a subscriber present at the start still fires once even
// if another handler removes it mid-call. It stops firing on the next Publish,
// once the removal's snapshot is the one in effect.
func TestEventBusUnsubscribeDuringPublish(t *testing.T) {
	bus := NewEventBus()

	var bCount int
	bUnsubscribe := bus.Subscribe(func(types.FlowEvent) { bCount++ })
	bus.Subscribe(func(types.FlowEvent) { bUnsubscribe() })

	bus.Publish(types.FlowEvent{Kind: types.FlowEventCompleted})
	if bCount != 1 {
		t.Fatalf("bCount = %d after first Publish, want 1 (subscriber was in the snapshot)", bCount)
	}

	bus.Publish(types.FlowEvent{Kind: types.FlowEventCompleted})
	if bCount != 1 {
		t.Fatalf("bCount = %d after second Publish, want 1 (subscriber was removed before this snapshot)", bCount)
	}
}
