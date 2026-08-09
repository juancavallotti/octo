package core_test

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The guard every emitter is expected to write is `if core.Tracer().Enabled()`,
// and it sits on paths that run per message and per block. What has to hold is
// that a runtime with tracing off pays an atomic load and allocates nothing —
// otherwise the feature costs something in every deployment that never turns it
// on. These run parallel because that is how the guard runs: on every flow
// worker at once, against one shared pointer.

// BenchmarkTracerDisabled is the shipped cost of tracing being off.
func BenchmarkTracerDisabled(b *testing.B) {
	previous := core.Tracer()
	core.SetTracer(nil)
	b.Cleanup(func() { core.SetTracer(previous) })

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if core.Tracer().Enabled() {
				b.Fatal("the no-op tracer reported enabled")
			}
		}
	})
}

// BenchmarkTracerPublish is the enabled path: stamp a sequence number and hand
// the record to the drain goroutine. The buffer is large enough that this
// measures the send rather than the drop.
func BenchmarkTracerPublish(b *testing.B) {
	sink := discardSink{}
	tracer := core.NewBufferedTracer(sink, core.TraceOptions{Enabled: true, Buffer: 1 << 16})
	b.Cleanup(func() { _ = tracer.Close() })

	event := types.TraceEvent{
		Kind:      types.TraceBlockPostInvoke,
		Flow:      "orders",
		Path:      "orders.charge",
		BlockType: "rest",
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tracer.Publish(event)
		}
	})
}

// discardSink accepts everything and keeps nothing, so a benchmark measures the
// publisher rather than whatever a real sink does with the record.
type discardSink struct{}

func (discardSink) Write(types.TraceEvent) error { return nil }
func (discardSink) Flush() error                 { return nil }
func (discardSink) Close() error                 { return nil }
