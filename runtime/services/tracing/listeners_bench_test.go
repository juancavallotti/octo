package tracing

import (
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The block listener runs inline, on every flow worker at once, twice per traced
// block per message. These run parallel for that reason, and the gap between them
// is the whole cost argument for --traces-bodies=false: capturing payloads means
// marshalling the message once per block, which dominates everything else the
// listener does.

// discardTracer is enabled and keeps nothing, so a benchmark measures the
// listener rather than a sink.
type discardTracer struct{}

func (discardTracer) Enabled() bool              { return true }
func (discardTracer) Publish(_ types.TraceEvent) {}

func benchEvent(b *testing.B) types.BlockEvent {
	b.Helper()
	msg, err := types.NewMessage("corr-1")
	if err != nil {
		b.Fatalf("new message: %v", err)
	}
	msg.Body = map[string]any{
		"orderId": "A-1001",
		"amount":  float64(129.95),
		"items":   []any{"widget", "gasket"},
	}
	msg.Variables.Set("httpStatus", 200)
	msg.Variables.Set("method", "POST")
	if _, err := msg.EnsureTraceID(); err != nil {
		b.Fatalf("ensure trace id: %v", err)
	}
	return types.BlockEvent{
		Kind:       types.BlockPostInvoke,
		Flow:       "orders",
		Path:       "orders.charge",
		BlockType:  "rest",
		EventID:    msg.EventID,
		OccurredAt: time.Now(),
		Duration:   3 * time.Millisecond,
		Message:    msg,
	}
}

func benchListener(b *testing.B, s *Service) {
	b.Helper()
	previous := core.Tracer()
	core.SetTracer(discardTracer{})
	b.Cleanup(func() { core.SetTracer(previous) })

	event := benchEvent(b)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := b.Context()
		for pb.Next() {
			s.onBlockEvent(ctx, event)
		}
	})
}

// BenchmarkBlockListenerWithPayloads is the shipped default: body and variables
// marshalled inline, per block, per message.
func BenchmarkBlockListenerWithPayloads(b *testing.B) {
	benchListener(b, &Service{enabled: true, bodies: true, vars: true, maxPayload: defaultMaxPayload})
}

// BenchmarkBlockListenerSequenceOnly is --traces-bodies=false --traces-vars=false:
// the record still names the flow, the block, the timing and the ids, which is
// everything needed to reconstruct the path.
func BenchmarkBlockListenerSequenceOnly(b *testing.B) {
	benchListener(b, &Service{enabled: true, maxPayload: defaultMaxPayload})
}
