package observability

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// watchAll is the --metrics-blocks value that watches every block instead of a
// named set. It is a foot-gun with a warning rather than a forbidden value: on a
// small config it is the fastest way to find where the time goes.
const watchAll = "*"

// Per-block label names, on top of the flow-level ones.
const (
	labelPath = "path"
	labelType = "type"
)

// Block outcomes. They are not the flow outcomes: a block that returned a message
// succeeded even if the flow later failed somewhere else.
const (
	outcomeOK      = "ok"
	outcomeDropped = "dropped"
	outcomeError   = "error"
)

// The per-block duration histogram is finer at the bottom than the flow one: a
// block is usually one of the small numbers a flow's total is made of. These
// buckets run from half a millisecond to roughly four seconds.
const (
	blockBucketStart  = 0.0005
	blockBucketFactor = 2
	blockBucketCount  = 14
)

// blockMetrics reports per-block timings for an explicitly named set of block
// addresses.
//
// It is opt-in, and per-address rather than per-flow, because it is the expensive
// half of the feature twice over. Registering ANY async block listener flips
// core.BlockEvents.Active() process-wide, so the engine starts emitting a pre- and
// post-invoke event around every block in every flow, not just the watched ones —
// that is the real cost, and it is why per-flow metrics are on with --metrics and
// these are not. And a label per block path is a much larger series count than a
// label per flow.
type blockMetrics struct {
	// watch is the set of block addresses to report, or nil when watching all.
	watch map[string]struct{}
	all   bool

	invocations *prometheus.CounterVec
	duration    *prometheus.HistogramVec
}

// newBlockMetrics builds the per-block collectors for the given addresses and
// registers them on reg. It returns nil when no address was named, which is what
// keeps the block-event dispatcher inactive by default.
func newBlockMetrics(reg *prometheus.Registry, addresses []string, events *core.BlockEvents) *blockMetrics {
	if len(addresses) == 0 {
		return nil
	}

	b := &blockMetrics{
		watch: make(map[string]struct{}, len(addresses)),

		invocations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "block_invocations_total",
			Help:      "Invocations of a watched block, by outcome.",
		}, []string{labelFlow, labelPath, labelType, labelOutcome}),

		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "block_duration_seconds",
			Help: "How long a watched block took. A composite's time includes its children's, " +
				"which emit their own events, so summing across paths double-counts.",
			Buckets: prometheus.ExponentialBuckets(blockBucketStart, blockBucketFactor, blockBucketCount),
		}, []string{labelFlow, labelPath, labelType}),
	}

	for _, address := range addresses {
		if address == watchAll {
			b.all = true
			continue
		}
		b.watch[address] = struct{}{}
	}

	reg.MustRegister(b.invocations, b.duration)

	// The dispatcher owns the running total of what it discarded, so read it at
	// scrape time instead of keeping a second copy in step with it. Async delivery
	// is at-most-once, so a non-zero value here means the counters above are
	// under-reporting — which is worth knowing and impossible to infer otherwise.
	reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "block_events_dropped_total",
		Help: "Block events discarded because the dispatcher's queue was full. " +
			"Non-zero means the block counters are under-reporting.",
	}, func() float64 {
		if events == nil {
			return 0
		}
		return float64(events.Dropped())
	}))

	return b
}

// watches reports whether a block path is one this is reporting on.
func (b *blockMetrics) watches(path string) bool {
	if b.all {
		return true
	}
	_, ok := b.watch[path]
	return ok
}

// onBlockEvent records one watched block invocation. It runs on the dispatcher's
// own goroutine, after the fact, so a slow update costs telemetry rather than
// throughput — and it must not dereference the event's message, which the flow has
// long since moved on and started mutating.
func (b *blockMetrics) onBlockEvent(event types.BlockEvent) {
	if event.Kind != types.BlockPostInvoke || !b.watches(event.Path) {
		return
	}
	b.invocations.WithLabelValues(event.Flow, event.Path, event.BlockType, blockOutcome(event)).Inc()
	b.duration.WithLabelValues(event.Flow, event.Path, event.BlockType).Observe(event.Duration.Seconds())
}

// blockOutcome classifies a post-invoke event: a block either failed, filtered the
// message out, or returned one.
func blockOutcome(event types.BlockEvent) string {
	switch {
	case event.Err != nil:
		return outcomeError
	case event.Dropped:
		return outcomeDropped
	default:
		return outcomeOK
	}
}

// parseAddresses splits a comma-separated --metrics-blocks value, trimming blanks.
func parseAddresses(raw string) []string {
	var addresses []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			addresses = append(addresses, trimmed)
		}
	}
	return addresses
}
