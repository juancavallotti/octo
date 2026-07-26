package core_test

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The engine asks Observes once per block, on every message, from every flow
// worker at once. These pin what that question costs in the three states a
// deployment is actually in: nothing registered (the default), something
// registered but not this block, and this block watched.
//
// They run parallel because the interesting property is not the per-call cost but
// whether it scales: the read path must not write to shared memory, or the whole
// point of measuring inline is lost to cache-line contention.

func benchObserves(b *testing.B, events *core.BlockEvents, path string) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if events.Observes(path) {
				events.Emit(context.Background(), types.BlockEvent{
					Kind: types.BlockPostInvoke, Flow: "orders", Path: path, BlockType: "rest",
				})
			}
		}
	})
}

// The default: --metrics-blocks unset, so nothing is registered.
func BenchmarkBlockEventsNoListeners(b *testing.B) {
	benchObserves(b, core.NewBlockEvents(), "orders.charge")
}

// A watched block exists, but this is not it — the case every other block in the
// config is in, and the one that has to stay free.
func BenchmarkBlockEventsUnwatchedBlock(b *testing.B) {
	events := core.NewBlockEvents()
	events.AddSyncFor([]string{"orders.charge"}, func(context.Context, types.BlockEvent) {})
	benchObserves(b, events, "orders.validate")
}

// The watched block itself: the question is asked, the event is built, and the
// listener runs inline.
func BenchmarkBlockEventsWatchedBlock(b *testing.B) {
	events := core.NewBlockEvents()
	events.AddSyncFor([]string{"orders.charge"}, func(context.Context, types.BlockEvent) {})
	benchObserves(b, events, "orders.charge")
}
