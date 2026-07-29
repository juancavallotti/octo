package core

import (
	"fmt"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// BenchmarkEventBusPublish measures Publish under concurrent load with a
// handful of subscribers, the shape the flow-event bus sees in practice: a
// small, startup-fixed subscriber list read on every message. Run with
// varying GOMAXPROCS to see the copy-on-write Publish path stay flat instead
// of degrading with core count:
//
//	go test ./runtime/core/... -run '^$' -bench BenchmarkEventBusPublish -benchtime 1000000x -cpu 1,2,4,8,10
func BenchmarkEventBusPublish(b *testing.B) {
	noop := func(types.FlowEvent) {}

	for _, subs := range []int{1, 2, 4} {
		bus := NewEventBus()
		for range subs {
			bus.Subscribe(noop)
		}

		b.Run(fmt.Sprintf("subs=%d", subs), func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					bus.Publish(types.FlowEvent{})
				}
			})
		})
	}
}
