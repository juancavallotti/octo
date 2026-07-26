package observability

import (
	"context"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

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
// It is opt-in, and per-address rather than per-flow, because watching a block
// costs something on the flow's own goroutine — the engine builds an event around
// it and this records it inline — and because a label per block path is a much
// larger series count than a label per flow. Naming addresses keeps both bounded
// by what someone asked for. `*` gives that up on purpose.
type blockMetrics struct {
	// watch is the set of block addresses to report, or empty when watching all.
	watch map[string]struct{}
	all   bool

	invocations *prometheus.CounterVec
	duration    *prometheus.HistogramVec
}

// newBlockMetrics builds the per-block collectors for the given addresses and
// registers them on reg. It returns nil when no address was named, which is what
// keeps the block-event dispatcher inactive by default.
func newBlockMetrics(reg *prometheus.Registry, addresses []string) *blockMetrics {
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
	return b
}

// paths returns the addresses to register with the dispatcher, or nil to ask for
// every block. Registering the set is what keeps an unwatched block from being
// built into an event at all — the filter belongs on the producer, not here.
func (b *blockMetrics) paths() []string {
	if b.all {
		return nil
	}
	addresses := make([]string, 0, len(b.watch))
	for address := range b.watch {
		addresses = append(addresses, address)
	}
	return addresses
}

// watches reports whether a block path is one this is reporting on.
func (b *blockMetrics) watches(path string) bool {
	if b.all {
		return true
	}
	_, ok := b.watch[path]
	return ok
}

// onBlockEvent records one watched block invocation. It is a sync listener: it
// runs on the flow's own goroutine, in the middle of the block it is timing, so
// everything it does is on the message's critical path: a label lookup and two
// atomic adds, and nothing that allocates or can block.
//
// Inline is what makes the numbers true. Recording these off the goroutine meant
// a queue between the flow and the counters, and a queue between a fleet of
// producers and one consumer drops under exactly the load worth measuring.
func (b *blockMetrics) onBlockEvent(_ context.Context, event types.BlockEvent) {
	// A watched block still emits a pre-invoke, which carries no duration and no
	// outcome; counting it would double every number.
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
