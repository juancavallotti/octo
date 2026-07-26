package observability

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/juancavallotti/octo/runtime/types"
)

// The per-block update runs inline, on every flow worker at once, so what matters
// is that it does not write to shared memory. These run parallel for that reason:
// a per-call cost that looks fine on one core and collapses on ten is the failure
// mode worth catching.

// BenchmarkBlockMetricsRecord is the shipped path: the label tuple resolves out of
// the copy-on-write cache, and the update is two atomic adds.
func BenchmarkBlockMetricsRecord(b *testing.B) {
	m := newBlockMetrics(prometheus.NewRegistry(), []string{watchAll})
	event := blockEvent("orders", "orders.charge", "rest", 3*time.Millisecond, nil, false)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := b.Context()
		for pb.Next() {
			m.onBlockEvent(ctx, event)
		}
	})
}

// BenchmarkBlockMetricsRecordUncached is the same update going through
// WithLabelValues every time, which is what the cache replaced. The gap is the
// vector's label hashing and the read lock on its map — one cache line, written by
// every core on every block.
func BenchmarkBlockMetricsRecordUncached(b *testing.B) {
	m := newBlockMetrics(prometheus.NewRegistry(), []string{watchAll})
	event := blockEvent("orders", "orders.charge", "rest", 3*time.Millisecond, nil, false)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			outcome := blockOutcome(event)
			m.invocations.WithLabelValues(event.Flow, event.Path, event.BlockType, outcome).Inc()
			m.duration.WithLabelValues(event.Flow, event.Path, event.BlockType).
				Observe(event.Duration.Seconds())
		}
	})
}

// A pre-invoke event for a watched block is dispatched and discarded here, so the
// rejection has to be cheap too.
func BenchmarkBlockMetricsRejectsPreInvoke(b *testing.B) {
	m := newBlockMetrics(prometheus.NewRegistry(), []string{watchAll})
	event := types.BlockEvent{
		Kind: types.BlockPreInvoke, Flow: "orders", Path: "orders.charge", BlockType: "rest",
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := b.Context()
		for pb.Next() {
			m.onBlockEvent(ctx, event)
		}
	})
}
