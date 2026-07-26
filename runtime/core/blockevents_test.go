package core_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func preEvent(path string) types.BlockEvent {
	return types.BlockEvent{Kind: types.BlockPreInvoke, Flow: "orders", Path: path}
}

func TestBlockEventsInactiveUntilRegistered(t *testing.T) {
	events := core.NewBlockEvents()
	if events.Active() {
		t.Fatal("a dispatcher with no listeners reports active")
	}
	events.AddSync(nil)
	events.AddAsync(nil)
	if events.Active() {
		t.Fatal("nil listeners made the dispatcher active")
	}

	events.AddSync(func(context.Context, types.BlockEvent) {})
	if !events.Active() {
		t.Fatal("a registered sync listener did not make the dispatcher active")
	}
}

// TestBlockEventsSyncRunsInline pins the sync contract: the listener has run, in
// registration order, by the time Emit returns.
func TestBlockEventsSyncRunsInline(t *testing.T) {
	events := core.NewBlockEvents()

	var order []string
	events.AddSync(func(_ context.Context, ev types.BlockEvent) {
		order = append(order, "first:"+ev.Path)
	})
	events.AddSync(func(_ context.Context, ev types.BlockEvent) {
		order = append(order, "second:"+ev.Path)
	})

	events.Emit(context.Background(), preEvent("orders.charge"))

	want := []string{"first:orders.charge", "second:orders.charge"}
	if len(order) != len(want) {
		t.Fatalf("calls = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("calls = %v, want %v", order, want)
		}
	}
}

// TestBlockEventsAsyncDeliversSequentially checks every event reaches every async
// listener, in emit order, on one goroutine — a listener may keep unsynchronized
// state, so overlap would be a broken contract, not just a surprise.
func TestBlockEventsAsyncDeliversSequentially(t *testing.T) {
	events := core.NewBlockEvents()
	defer events.Stop()

	var (
		seen    []string
		inside  bool
		overlap bool
	)
	events.AddAsync(func(ev types.BlockEvent) {
		if inside {
			overlap = true
		}
		inside = true
		seen = append(seen, ev.Path)
		inside = false
	})

	for _, path := range []string{"orders.a", "orders.b", "orders.c"} {
		events.Emit(context.Background(), preEvent(path))
	}
	events.Stop()

	if overlap {
		t.Error("async listeners overlapped; delivery must be sequential")
	}
	want := []string{"orders.a", "orders.b", "orders.c"}
	if len(seen) != len(want) {
		t.Fatalf("delivered %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("delivered %v, want %v", seen, want)
		}
	}
}

// TestBlockEventsDropsWhenAsyncFallsBehind is the backpressure contract: Emit
// returns promptly even while a listener is wedged, and says how much it lost.
func TestBlockEventsDropsWhenAsyncFallsBehind(t *testing.T) {
	events := core.NewBlockEvents()
	defer events.Stop()

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	events.AddAsync(func(types.BlockEvent) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})

	// Wedge the pump, then emit far more than the queue can hold.
	events.Emit(context.Background(), preEvent("orders.wedge"))
	<-entered

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 4096; i++ {
			events.Emit(context.Background(), preEvent("orders.flood"))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Emit blocked while an async listener was wedged")
	}

	if events.Dropped() == 0 {
		t.Error("flooding a wedged listener dropped nothing")
	}
	close(release)
}

// TestBlockEventsAsyncPanicDoesNotKillThePump checks one bad listener costs its own
// event rather than every future event in the process.
func TestBlockEventsAsyncPanicDoesNotKillThePump(t *testing.T) {
	events := core.NewBlockEvents()
	defer events.Stop()

	var delivered int
	events.AddAsync(func(types.BlockEvent) { panic("boom") })
	events.AddAsync(func(types.BlockEvent) { delivered++ })

	events.Emit(context.Background(), preEvent("orders.a"))
	events.Emit(context.Background(), preEvent("orders.b"))
	events.Stop()

	if delivered != 2 {
		t.Fatalf("delivered %d events after a panicking listener, want 2", delivered)
	}
}

// TestBlockEventsRegisterWhileEmitting exercises the copy-on-write registration
// against a concurrent emit path; it is the case -race is here to check.
func TestBlockEventsRegisterWhileEmitting(t *testing.T) {
	events := core.NewBlockEvents()
	defer events.Stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			events.Emit(context.Background(), preEvent("orders.a"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			events.AddSync(func(context.Context, types.BlockEvent) {})
			events.AddAsync(func(types.BlockEvent) {})
		}
	}()
	wg.Wait()
}

// TestBlockEventsStopIsIdempotent guards the drain path against a second call,
// which would otherwise close an already-closed channel.
func TestBlockEventsStopIsIdempotent(t *testing.T) {
	events := core.NewBlockEvents()
	events.AddAsync(func(types.BlockEvent) {})
	events.Stop()
	events.Stop()
}
