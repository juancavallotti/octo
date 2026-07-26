package core_test

import (
	"context"
	"sync"
	"testing"

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
	events.AddSyncFor([]string{"orders.charge"}, nil)
	if events.Active() {
		t.Fatal("nil listeners made the dispatcher active")
	}

	events.AddSync(func(context.Context, types.BlockEvent) {})
	if !events.Active() {
		t.Fatal("a registered sync listener did not make the dispatcher active")
	}
}

// TestBlockEventsSyncRunsInline pins the delivery contract: every listener has
// run, in registration order, by the time Emit returns. Nothing is queued and
// nothing is dropped.
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

// TestBlockEventsObservesOnlyRegisteredPaths is the whole point of the filter: the
// engine asks before it builds an event, so a block nobody named never becomes one.
func TestBlockEventsObservesOnlyRegisteredPaths(t *testing.T) {
	events := core.NewBlockEvents()

	if events.Observes("orders.charge") {
		t.Fatal("a dispatcher with no listeners observes a path")
	}

	events.AddSyncFor([]string{"orders.charge"}, func(context.Context, types.BlockEvent) {})
	if !events.Observes("orders.charge") {
		t.Error("a registered path is not observed")
	}
	if events.Observes("orders.validate") {
		t.Error("an unregistered path is observed; every block would be built into an event")
	}

	// A second listener widens the set without disturbing the first.
	events.AddSyncFor([]string{"audit.log"}, func(context.Context, types.BlockEvent) {})
	for _, path := range []string{"orders.charge", "audit.log"} {
		if !events.Observes(path) {
			t.Errorf("path %q is not observed after a second registration", path)
		}
	}
	if events.Observes("orders.validate") {
		t.Error("a second filtered listener made an unrelated path observed")
	}
}

// An unfiltered listener wants everything, so Observes must answer yes for a path
// nobody named.
func TestBlockEventsUnfilteredListenerObservesEverything(t *testing.T) {
	events := core.NewBlockEvents()
	events.AddSyncFor([]string{"orders.charge"}, func(context.Context, types.BlockEvent) {})
	events.AddSync(func(context.Context, types.BlockEvent) {})

	if !events.Observes("anything.at.all") {
		t.Error("an unfiltered listener did not make every path observed")
	}
}

// Registering for no path registers nothing. Treating an empty set as "everything"
// is how watching one address would turn into watching the whole process.
func TestBlockEventsEmptyPathSetRegistersNothing(t *testing.T) {
	events := core.NewBlockEvents()
	events.AddSyncFor(nil, func(context.Context, types.BlockEvent) {})

	if events.Active() {
		t.Fatal("registering for no path made the dispatcher active")
	}
	if events.Observes("orders.charge") {
		t.Error("registering for no path observed a block")
	}
}

// A filtered listener must not see the blocks it did not ask for, even when
// another listener widened what the dispatcher observes.
func TestBlockEventsDeliversOnlyWhatEachListenerAskedFor(t *testing.T) {
	events := core.NewBlockEvents()

	var charge, all []string
	events.AddSyncFor([]string{"orders.charge"}, func(_ context.Context, ev types.BlockEvent) {
		charge = append(charge, ev.Path)
	})
	events.AddSync(func(_ context.Context, ev types.BlockEvent) {
		all = append(all, ev.Path)
	})

	for _, path := range []string{"orders.charge", "orders.validate"} {
		events.Emit(context.Background(), preEvent(path))
	}

	if len(charge) != 1 || charge[0] != "orders.charge" {
		t.Errorf("filtered listener saw %v, want only [orders.charge]", charge)
	}
	if len(all) != 2 {
		t.Errorf("unfiltered listener saw %v, want both blocks", all)
	}
}

// A listener is an observer, so a broken one must not change the flow's outcome.
// Emit runs on a flow worker, and a panic unwinding through one takes the process
// with it — so containing it is the difference between losing a metric and losing
// the runtime.
func TestBlockEventsContainsAListenerPanic(t *testing.T) {
	events := core.NewBlockEvents()

	var after int
	events.AddSync(func(context.Context, types.BlockEvent) { panic("boom") })
	events.AddSync(func(context.Context, types.BlockEvent) { after++ })

	events.Emit(context.Background(), preEvent("orders.a"))
	events.Emit(context.Background(), preEvent("orders.b"))

	// The listener registered after the panicking one still ran, both times: one
	// bad listener costs its own event, not every listener's.
	if after != 2 {
		t.Errorf("listeners after a panicking one ran %d times, want 2", after)
	}
	if got := events.Panics(); got != 2 {
		t.Errorf("Panics() = %d, want 2 — a swallowed panic must stay countable", got)
	}
}

// A panic must not leave the dispatcher wedged: the next event is delivered
// normally, and a listener that recovers on its own is not counted.
func TestBlockEventsPanicCountIsOnlyForSwallowedPanics(t *testing.T) {
	events := core.NewBlockEvents()

	var seen int
	events.AddSync(func(context.Context, types.BlockEvent) {
		defer func() { _ = recover() }()
		seen++
		panic("handled by the listener itself")
	})

	events.Emit(context.Background(), preEvent("orders.a"))

	if seen != 1 {
		t.Errorf("listener ran %d times, want 1", seen)
	}
	if got := events.Panics(); got != 0 {
		t.Errorf("Panics() = %d, want 0 — the listener recovered its own panic", got)
	}
}

// TestBlockEventsRegisterWhileEmitting exercises the copy-on-write registration
// against a concurrent emit path; it is the case -race is here to check.
func TestBlockEventsRegisterWhileEmitting(t *testing.T) {
	events := core.NewBlockEvents()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			events.Emit(context.Background(), preEvent("orders.a"))
			events.Observes("orders.a")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			events.AddSync(func(context.Context, types.BlockEvent) {})
			events.AddSyncFor([]string{"orders.a"}, func(context.Context, types.BlockEvent) {})
		}
	}()
	wg.Wait()
}
