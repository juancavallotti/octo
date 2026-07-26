package observability

import (
	"errors"
	"flag"
	"io"
	"strings"
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
	if got := newBlockMetrics(prometheus.NewRegistry(), nil, nil); got != nil {
		t.Error("newBlockMetrics with no addresses returned a collector; nothing should be watched")
	}
}

// Only the named blocks are reported. Everything else still reaches the listener —
// the engine emits for every block once anything is watched — and is discarded.
func TestBlockMetricsReportsOnlyWatchedPaths(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{"orders.charge"}, nil)

	b.onBlockEvent(blockEvent("orders", "orders.charge", "rest", 10*time.Millisecond, nil, false))
	b.onBlockEvent(blockEvent("orders", "orders.validate", "filter", time.Millisecond, nil, false))
	b.onBlockEvent(blockEvent("audit", "audit.log", "log", time.Millisecond, nil, false))

	if got := testutil.ToFloat64(b.invocations.WithLabelValues("orders", "orders.charge", "rest", outcomeOK)); got != 1 {
		t.Errorf("watched block invocations = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(b.invocations); got != 1 {
		t.Errorf("series = %d, want 1 — only the watched address may be reported", got)
	}
}

func TestBlockMetricsWatchAll(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{watchAll}, nil)

	b.onBlockEvent(blockEvent("orders", "orders.charge", "rest", time.Millisecond, nil, false))
	b.onBlockEvent(blockEvent("audit", "audit.log", "log", time.Millisecond, nil, false))

	if !b.all {
		t.Error("* did not set watch-all")
	}
	if got := testutil.CollectAndCount(b.invocations); got != 2 {
		t.Errorf("series = %d, want 2 — * reports every block", got)
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
	b := newBlockMetrics(reg, []string{"orders.charge"}, nil)

	b.onBlockEvent(types.BlockEvent{
		Kind: types.BlockPreInvoke, Flow: "orders", Path: "orders.charge", BlockType: "rest",
	})

	if got := testutil.CollectAndCount(b.invocations); got != 0 {
		t.Errorf("series after only a pre-invoke = %d, want 0", got)
	}
}

func TestBlockMetricsObservesDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newBlockMetrics(reg, []string{"orders.charge"}, nil)

	b.onBlockEvent(blockEvent("orders", "orders.charge", "rest", 4*time.Millisecond, nil, false))
	b.onBlockEvent(blockEvent("orders", "orders.charge", "rest", 8*time.Millisecond, nil, false))

	metric := b.duration.WithLabelValues("orders", "orders.charge", "rest")
	if got := testutil.CollectAndCount(b.duration); got != 1 {
		t.Fatalf("duration series = %d, want 1", got)
	}
	if metric == nil {
		t.Fatal("no duration series for the watched block")
	}
}

// The dispatcher owns the running total of what it dropped; the counter reads it
// at scrape time, so it can never lag behind.
func TestBlockEventsDroppedIsReadFromTheDispatcher(t *testing.T) {
	events := core.NewBlockEvents()
	reg := prometheus.NewRegistry()
	newBlockMetrics(reg, []string{"orders.charge"}, events)

	// A listener that never returns fills the queue, so Emit starts dropping.
	block := make(chan struct{})
	events.AddAsync(func(types.BlockEvent) { <-block })
	for i := 0; i < 2048; i++ {
		events.Emit(t.Context(), blockEvent("orders", "orders.charge", "rest", 0, nil, false))
	}
	close(block)

	if events.Dropped() == 0 {
		t.Skip("the dispatcher drained faster than the test could fill it")
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == "octo_block_events_dropped_total" {
			if got := family.GetMetric()[0].GetCounter().GetValue(); got != float64(events.Dropped()) {
				t.Errorf("octo_block_events_dropped_total = %v, want the dispatcher's %d", got, events.Dropped())
			}
			return
		}
	}
	t.Fatal("octo_block_events_dropped_total is not registered")
}

// Watching nothing must leave the process-wide dispatcher inactive: the whole cost
// of the feature when it is off is that Active() stays false.
func TestNoBlockAddressesLeavesDispatcherInactive(t *testing.T) {
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
