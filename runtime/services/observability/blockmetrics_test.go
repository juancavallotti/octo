package observability

import (
	"errors"
	"flag"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// blockEvent builds a post-invoke event for a block.
func blockEvent(flow, path, blockType string, d time.Duration, err error, dropped bool) types.BlockEvent {
	return types.BlockEvent{
		Kind:      types.BlockPostInvoke,
		Flow:      flow,
		Path:      path,
		BlockType: blockType,
		Duration:  d,
		Err:       err,
		Dropped:   dropped,
	}
}

func TestParseAddresses(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "one", raw: "orders.charge", want: []string{"orders.charge"}},
		{
			name: "several, with a bracketed branch",
			raw:  "orders.charge,orders.fanout[audit].log-it",
			want: []string{"orders.charge", "orders.fanout[audit].log-it"},
		},
		{name: "whitespace is trimmed", raw: " orders.charge , audit.log ", want: []string{"orders.charge", "audit.log"}},
		{name: "blanks are dropped", raw: "orders.charge,,", want: []string{"orders.charge"}},
		{name: "only blanks", raw: " , , ", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAddresses(tt.raw)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("parseAddresses(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// Naming no address registers nothing, which is what keeps the block-event
// dispatcher — and the engine's per-block emission — off by default.
func TestNewBlockMetricsIsNilWithoutAddresses(t *testing.T) {
	if got := newBlockMetrics(prometheus.NewRegistry(), nil); got != nil {
		t.Error("newBlockMetrics with no addresses returned a collector; nothing should be watched")
	}
}

// Only the named blocks are reported. The dispatcher filters too, so an unwatched
// block does not normally reach the listener at all; this pins the listener's own
// guard, which is what makes it correct in isolation.
func TestBlockMetricsReportsOnlyWatchedPaths(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{"orders.charge"})

	b.onBlockEvent(t.Context(), blockEvent("orders", "orders.charge", "rest", 10*time.Millisecond, nil, false))
	b.onBlockEvent(t.Context(), blockEvent("orders", "orders.validate", "filter", time.Millisecond, nil, false))
	b.onBlockEvent(t.Context(), blockEvent("audit", "audit.log", "log", time.Millisecond, nil, false))

	if got := testutil.ToFloat64(b.invocations.WithLabelValues("orders", "orders.charge", "rest", outcomeOK)); got != 1 {
		t.Errorf("watched block invocations = %v, want 1", got)
	}
	// Resolving a block pre-creates its counter for all three outcomes, so one
	// watched block is three series and an unwatched one is none.
	if got := testutil.CollectAndCount(b.invocations); got != 3 {
		t.Errorf("series = %d, want 3 — only the watched address may be reported", got)
	}
}

// The addresses handed to the dispatcher are what keeps an unwatched block from
// being built into an event. '*' asks for everything instead.
func TestBlockMetricsPaths(t *testing.T) {
	named := newBlockMetrics(prometheus.NewRegistry(), []string{"orders.charge"})
	if got := named.paths(); len(got) != 1 || got[0] != "orders.charge" {
		t.Errorf("paths() = %v, want [orders.charge]", got)
	}

	all := newBlockMetrics(prometheus.NewRegistry(), []string{watchAll})
	if got := all.paths(); got != nil {
		t.Errorf("paths() = %v with *, want nil — * registers unfiltered", got)
	}
}

func TestBlockMetricsWatchAll(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{watchAll})

	b.onBlockEvent(t.Context(), blockEvent("orders", "orders.charge", "rest", time.Millisecond, nil, false))
	b.onBlockEvent(t.Context(), blockEvent("audit", "audit.log", "log", time.Millisecond, nil, false))

	if !b.all {
		t.Error("* did not set watch-all")
	}
	if got := testutil.CollectAndCount(b.invocations); got != 6 {
		t.Errorf("series = %d, want 6 — * reports every block, three outcomes each", got)
	}
}

// The resolved collectors are cached per block, and the cache must survive the
// concurrent first-sighting that every flow worker races into on startup.
func TestBlockMetricsSeriesCacheIsStableUnderConcurrency(t *testing.T) {
	b := newBlockMetrics(prometheus.NewRegistry(), []string{watchAll})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.onBlockEvent(t.Context(), blockEvent("orders", "orders.charge", "rest", time.Millisecond, nil, false))
			}
		}()
	}
	wg.Wait()

	if got := testutil.ToFloat64(b.invocations.WithLabelValues("orders", "orders.charge", "rest", outcomeOK)); got != 800 {
		t.Errorf("invocations = %v, want 800 — every inline update must land", got)
	}
	if got := len(*b.series.Load()); got != 1 {
		t.Errorf("cached series = %d, want 1 — one block resolves once", got)
	}
}

// A block's outcome is its own: it failed, filtered the message out, or returned
// one. That is not the flow's outcome — a block can succeed in a flow that later
// fails somewhere else.
func TestBlockMetricsOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		dropped bool
		want    string
	}{
		{name: "returned a message", want: outcomeOK},
		{name: "filtered it out", dropped: true, want: outcomeDropped},
		{name: "failed", err: errors.New("boom"), want: outcomeError},
		{name: "an error wins over dropped", err: errors.New("boom"), dropped: true, want: outcomeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blockOutcome(blockEvent("orders", "orders.charge", "rest", 0, tt.err, tt.dropped))
			if got != tt.want {
				t.Errorf("blockOutcome = %q, want %q", got, tt.want)
			}
		})
	}
}

// pre-invoke events carry no duration or outcome, so counting them would double
// every number.
func TestBlockMetricsIgnoresPreInvoke(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{"orders.charge"})

	b.onBlockEvent(t.Context(), types.BlockEvent{
		Kind: types.BlockPreInvoke, Flow: "orders", Path: "orders.charge", BlockType: "rest",
	})

	if got := testutil.CollectAndCount(b.invocations); got != 0 {
		t.Errorf("series after only a pre-invoke = %d, want 0", got)
	}
}

func TestBlockMetricsObservesDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{"orders.charge"})

	b.onBlockEvent(t.Context(), blockEvent("orders", "orders.charge", "rest", 4*time.Millisecond, nil, false))
	b.onBlockEvent(t.Context(), blockEvent("orders", "orders.charge", "rest", 8*time.Millisecond, nil, false))

	metric := b.duration.WithLabelValues("orders", "orders.charge", "rest")
	if got := testutil.CollectAndCount(b.duration); got != 1 {
		t.Fatalf("duration series = %d, want 1", got)
	}
	if metric == nil {
		t.Fatal("no duration series for the watched block")
	}
}

// Watching a named block must not make the engine observe anything else: the
// dispatcher is registered for that address only, so every other block is never
// built into an event.
func TestWatchedAddressesRegisterOnlyThosePaths(t *testing.T) {
	events := core.NewBlockEvents()
	b := newBlockMetrics(prometheus.NewRegistry(), []string{"orders.charge"})
	events.AddSyncFor(b.paths(), b.onBlockEvent)

	if !events.Observes("orders.charge") {
		t.Error("the watched address is not observed")
	}
	if events.Observes("orders.validate") {
		t.Error("an unwatched block is observed; every block would pay for the measurement")
	}
}

// Watching nothing must leave the process-wide dispatcher inactive: the whole cost
// of the feature when it is off is that Active() stays false.
func TestNoBlockAddressesLeavesDispatcherInactive(t *testing.T) {
	// --metrics-blocks defaults to the environment, so an ambient
	// OCTO_METRICS_BLOCKS would make this test watch blocks and assert the
	// opposite of what it says.
	t.Setenv(envMetricBlocks, "")

	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)
	if err := fs.Parse([]string{"--metrics"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	svc.collectors = newMetrics(nil)

	before := core.DefaultBlockEvents().Active()
	svc.watchBlocks()
	if core.DefaultBlockEvents().Active() != before {
		t.Error("watchBlocks with no addresses activated the block-event dispatcher")
	}
}
