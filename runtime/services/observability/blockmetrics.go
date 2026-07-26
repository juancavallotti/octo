package observability

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

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

	// series caches the collectors each block resolves to. Updates run inline on
	// every flow worker at once, and WithLabelValues hashes its labels and takes a
	// read lock on the vector's map — one shared cache line, written by every core
	// on every block. The cache turns that into an atomic load and a map read.
	//
	// It is copy-on-write: readers hold an immutable snapshot, and a miss rebuilds
	// it under fill. Growth is bounded by the number of distinct block paths in the
	// config, so it stops growing once traffic has touched each of them once.
	series atomic.Pointer[map[seriesKey]*blockSeries]
	fill   sync.Mutex
}

// seriesKey identifies one block's series. It is the label tuple that does not
// vary with the outcome.
type seriesKey struct {
	flow, path, blockType string
}

// blockSeries is the resolved collectors for one block: a counter per outcome and
// the duration observer. Resolving all three counters up front costs two extra
// lookups on a block's first invocation and saves one on every invocation after.
type blockSeries struct {
	ok, dropped, errored prometheus.Counter
	duration             prometheus.Observer
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
	b.series.Store(&map[seriesKey]*blockSeries{})

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
// everything it does is on the message's critical path. That budget buys a map
// read and two atomic adds, and nothing else — no allocation, no lock, and no
// call that can block.
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

	series := b.seriesFor(seriesKey{flow: event.Flow, path: event.Path, blockType: event.BlockType})
	series.counter(blockOutcome(event)).Inc()
	series.duration.Observe(event.Duration.Seconds())
}

// counter returns the pre-resolved counter for one outcome.
//
//nolint:ireturn // the prometheus API hands back the Counter interface
func (s *blockSeries) counter(outcome string) prometheus.Counter {
	switch outcome {
	case outcomeError:
		return s.errored
	case outcomeDropped:
		return s.dropped
	default:
		return s.ok
	}
}

// seriesFor returns the cached collectors for a block, resolving and caching them
// on its first invocation.
func (b *blockMetrics) seriesFor(key seriesKey) *blockSeries {
	if series, ok := (*b.series.Load())[key]; ok {
		return series
	}
	return b.resolve(key)
}

// resolve fills the cache for one block. It is the slow path — taken once per
// block path per process — so it takes a lock and rebuilds the map wholesale,
// leaving the read path free of both.
func (b *blockMetrics) resolve(key seriesKey) *blockSeries {
	b.fill.Lock()
	defer b.fill.Unlock()

	// Another goroutine may have resolved the same block while this one waited.
	current := *b.series.Load()
	if series, ok := current[key]; ok {
		return series
	}

	series := &blockSeries{
		ok:       b.invocations.WithLabelValues(key.flow, key.path, key.blockType, outcomeOK),
		dropped:  b.invocations.WithLabelValues(key.flow, key.path, key.blockType, outcomeDropped),
		errored:  b.invocations.WithLabelValues(key.flow, key.path, key.blockType, outcomeError),
		duration: b.duration.WithLabelValues(key.flow, key.path, key.blockType),
	}

	next := make(map[seriesKey]*blockSeries, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[key] = series
	b.series.Store(&next)
	return series
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
