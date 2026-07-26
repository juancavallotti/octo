package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// eventRecorder collects block events from a sync listener. The listener runs on
// the flow's goroutine — and on a pool goroutine per fork branch — so it takes a
// lock the same way a real one would have to.
type eventRecorder struct {
	mu     sync.Mutex
	events []types.BlockEvent
}

// listen returns a dispatcher recording into r. Sync registration is what makes the
// recording deterministic: everything is delivered before Process returns.
func (r *eventRecorder) listen() *core.BlockEvents {
	events := core.NewBlockEvents()
	events.AddSync(func(_ context.Context, ev types.BlockEvent) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.events = append(r.events, ev)
	})
	return events
}

// recorded returns a copy of what was collected.
func (r *eventRecorder) recorded() []types.BlockEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.BlockEvent(nil), r.events...)
}

// trace renders the recording as "<kind> <path>" lines, which is what the
// assertions below compare against.
func (r *eventRecorder) trace() []string {
	events := r.recorded()
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, fmt.Sprintf("%s %s", ev.Kind, ev.Path))
	}
	return out
}

// runWithEvents builds cfg with a recording dispatcher and runs one message
// through it, returning the recording. A block error is not a test failure here:
// the "fail" block is one of the outcomes under test.
//
// The pool is started because a fork submits its branches to it; the flow is done
// with it by the time Process returns, so stopping it on cleanup is safe.
func runWithEvents(t *testing.T, reg *core.BlockRegistry, cfg types.FlowConfig) *eventRecorder {
	t.Helper()
	p := pool.New(4, 64)
	p.Start()
	t.Cleanup(p.Stop)

	rec := &eventRecorder{}
	flow, err := BuildRoot(cfg, reg, p, nil, core.BlockDeps{Events: rec.listen()})
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	if _, err = flow.Process(context.Background(), mustMessage(t)); err != nil {
		t.Logf("flow returned %v (expected for the failure cases)", err)
	}
	return rec
}

func wantTrace(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("trace =\n  %v\nwant\n  %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trace =\n  %v\nwant\n  %v", got, want)
		}
	}
}

// TestBlockEventsBracketEachBlock is the core ordering contract: one pre and one
// post per block, in chain order, with the post of a block before the pre of the
// next.
func TestBlockEventsBracketEachBlock(t *testing.T) {
	rec := runWithEvents(t, testRegistry(), types.FlowConfig{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "pass", Name: "first"},
			{Type: "pass", Name: "second"},
		},
	})

	wantTrace(t, rec.trace(), []string{
		"pre-invoke orders.first", "post-invoke orders.first",
		"pre-invoke orders.second", "post-invoke orders.second",
	})
}

// TestBlockEventsPostReportsOutcome checks post fires for all three outcomes and
// says which one it was — without that a listener cannot compute an error rate.
func TestBlockEventsPostReportsOutcome(t *testing.T) {
	tests := []struct {
		name        string
		blockType   string
		wantErr     bool
		wantDropped bool
	}{
		{name: "returned a message", blockType: "pass"},
		{name: "dropped the message", blockType: "drop", wantDropped: true},
		{name: "failed", blockType: "fail", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := runWithEvents(t, testRegistry(), types.FlowConfig{
				Name:    "orders",
				Process: []types.BlockConfig{{Type: tt.blockType, Name: "target"}},
			})

			events := rec.recorded()
			if len(events) != 2 {
				t.Fatalf("events = %v, want a pre and a post", rec.trace())
			}
			post := events[1]
			if post.Kind != types.BlockPostInvoke {
				t.Fatalf("second event is %q, want post-invoke", post.Kind)
			}
			if (post.Err != nil) != tt.wantErr {
				t.Errorf("Err = %v, wantErr %v", post.Err, tt.wantErr)
			}
			if post.Dropped != tt.wantDropped {
				t.Errorf("Dropped = %v, want %v", post.Dropped, tt.wantDropped)
			}
			// Every outcome carries a message: the one the block produced, or its
			// input for the two outcomes that produce none.
			if post.Message == nil {
				t.Error("post event carries no message")
			}
			if post.Duration < 0 {
				t.Errorf("Duration = %v, want a non-negative measurement", post.Duration)
			}
		})
	}
}

// TestBlockEventsCarryMessageIdentity checks the fields that join a block event to
// the flow-event stream are copied onto every event.
func TestBlockEventsCarryMessageIdentity(t *testing.T) {
	rec := &eventRecorder{}
	flow, err := BuildRoot(
		types.FlowConfig{Name: "orders", Process: []types.BlockConfig{{Type: "pass", Name: "charge"}}},
		testRegistry(), pool.New(0, 0), nil, core.BlockDeps{Events: rec.listen()},
	)
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}

	msg := mustMessage(t)
	msg.CorrelationID = "corr-1"
	if _, err = flow.Process(context.Background(), msg); err != nil {
		t.Fatalf("process: %v", err)
	}

	for _, ev := range rec.recorded() {
		if ev.Flow != "orders" {
			t.Errorf("Flow = %q, want %q", ev.Flow, "orders")
		}
		if ev.BlockType != "pass" {
			t.Errorf("BlockType = %q, want %q", ev.BlockType, "pass")
		}
		if ev.EventID != msg.EventID {
			t.Errorf("EventID = %q, want %q", ev.EventID, msg.EventID)
		}
		if ev.CorrelationID != "corr-1" {
			t.Errorf("CorrelationID = %q, want %q", ev.CorrelationID, "corr-1")
		}
		if ev.Message != msg {
			t.Error("Message is not the live message the block was given")
		}
	}
}

// TestBlockEventsNestIntoComposites is what the whole build-time path exists for:
// a composite's own pre/post brackets its children's, and each child addresses
// through the branch it sits in.
func TestBlockEventsNestIntoComposites(t *testing.T) {
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "pass", Name: "inner"}}}

	rec := runWithEvents(t, testRegistry(), types.FlowConfig{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "enrich", Name: "lookup", Body: &body},
			{Type: "pass", Name: "after"},
		},
	})

	wantTrace(t, rec.trace(), []string{
		"pre-invoke orders.lookup",
		"pre-invoke orders.lookup[body].inner",
		"post-invoke orders.lookup[body].inner",
		"post-invoke orders.lookup",
		"pre-invoke orders.after",
		"post-invoke orders.after",
	})
}

// TestBlockEventsAddressEveryBranchSlot pins the minted path for each composite
// branch the resolver knows about. It is the table that fails the day a composite
// grows a slot the builder does not descend through with a branch.
func TestBlockEventsAddressEveryBranchSlot(t *testing.T) {
	leaf := func(name string) types.BlockConfig { return types.BlockConfig{Type: "pass", Name: name} }
	chain := func(name string) *types.FlowConfig {
		return &types.FlowConfig{Process: []types.BlockConfig{leaf(name)}}
	}

	tests := []struct {
		name  string
		block types.BlockConfig
		want  string
	}{
		{
			name: "handle-errors process",
			block: types.BlockConfig{
				Type: "handle-errors", Name: "guard",
				Process: []types.BlockConfig{leaf("work")}, Error: []types.BlockConfig{leaf("recover")},
			},
			want: "orders.guard[process].work",
		},
		{
			name: "if then",
			block: types.BlockConfig{
				Type: "if", Name: "check", Condition: "true", Then: chain("yes"), Else: chain("no"),
			},
			want: "orders.check[then].yes",
		},
		{
			name: "if else",
			block: types.BlockConfig{
				Type: "if", Name: "check", Condition: "false", Then: chain("yes"), Else: chain("no"),
			},
			want: "orders.check[else].no",
		},
		{
			name: "switch case by name",
			block: types.BlockConfig{
				Type: "switch", Name: "route",
				Cases: []types.CaseConfig{{
					When: "true",
					Flow: types.FlowConfig{Name: "premium", Process: []types.BlockConfig{leaf("vip")}},
				}},
			},
			want: "orders.route[premium].vip",
		},
		{
			name: "switch case by index",
			block: types.BlockConfig{
				Type: "switch", Name: "route",
				Cases: []types.CaseConfig{{
					When: "true",
					Flow: types.FlowConfig{Process: []types.BlockConfig{leaf("anon")}},
				}},
			},
			want: "orders.route[0].anon",
		},
		{
			name: "switch default",
			block: types.BlockConfig{
				Type: "switch", Name: "route",
				Cases:   []types.CaseConfig{{When: "false", Flow: *chain("never")}},
				Default: chain("fallback"),
			},
			want: "orders.route[default].fallback",
		},
		{
			name: "fork branch by name",
			block: types.BlockConfig{
				Type: "fork", Name: "fanout",
				Branches: []types.FlowConfig{{Name: "audit", Process: []types.BlockConfig{leaf("record")}}},
			},
			want: "orders.fanout[audit].record",
		},
		{
			name: "fork branch by index",
			block: types.BlockConfig{
				Type: "fork", Name: "fanout",
				Branches: []types.FlowConfig{{Process: []types.BlockConfig{leaf("record")}}},
			},
			want: "orders.fanout[0].record",
		},
		{
			name:  "enrich body",
			block: types.BlockConfig{Type: "enrich", Name: "lookup", Body: chain("fetch")},
			want:  "orders.lookup[body].fetch",
		},
		{
			name: "foreach body",
			block: types.BlockConfig{
				Type: "foreach", Name: "each", Items: "[1]", As: "item", Body: chain("handle"),
			},
			want: "orders.each[body].handle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := runWithEvents(t, testRegistry(), types.FlowConfig{
				Name: "orders", Process: []types.BlockConfig{tt.block},
			})
			if !contains(rec.trace(), "pre-invoke "+tt.want) {
				t.Fatalf("trace %v does not contain %q", rec.trace(), "pre-invoke "+tt.want)
			}
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestBlockEventsDurationExcludesSyncListeners pins that Duration measures the
// block, not the observing. A sync listener runs inline on the flow's goroutine,
// and the debugger it exists for parks there for as long as a human takes — which
// must not be reported as the block having taken that long.
func TestBlockEventsDurationExcludesSyncListeners(t *testing.T) {
	const park = 50 * time.Millisecond

	events := core.NewBlockEvents()
	var post types.BlockEvent
	events.AddSync(func(_ context.Context, ev types.BlockEvent) {
		switch ev.Kind {
		case types.BlockPreInvoke:
			time.Sleep(park) // stand in for a breakpoint holding the flow
		case types.BlockPostInvoke:
			post = ev
		}
	})

	flow, err := BuildRoot(
		types.FlowConfig{Name: "orders", Process: []types.BlockConfig{{Type: "pass", Name: "charge"}}},
		testRegistry(), pool.New(0, 0), nil, core.BlockDeps{Events: events},
	)
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	if _, err = flow.Process(context.Background(), mustMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	if post.Kind != types.BlockPostInvoke {
		t.Fatal("no post-invoke event was delivered")
	}
	if post.Duration >= park {
		t.Errorf("Duration = %v, which includes the %v a sync listener parked for", post.Duration, park)
	}
}

// TestBlockEventsOnlyReachTheWiredDispatcher pins that a flow emits to the
// dispatcher it was built with and to nothing else. The interesting half is the
// unwired one: if the engine ever fell back to core.DefaultBlockEvents() when
// BlockDeps carries none, every flow built straight through BuildRoot — every
// test in this package, and dolphin — would start firing an embedder's
// process-wide listeners.
//
// Both listeners are sync, so the assertion needs no waiting: everything that is
// going to be delivered has been by the time Process returns. Async delivery is
// the dispatcher's business, and is tested there.
func TestBlockEventsOnlyReachTheWiredDispatcher(t *testing.T) {
	cfg := types.FlowConfig{Name: "orders", Process: []types.BlockConfig{
		{Type: "enrich", Name: "lookup", Body: &types.FlowConfig{
			Process: []types.BlockConfig{{Type: "pass", Name: "inner"}},
		}},
	}}

	t.Run("no dispatcher wired", func(t *testing.T) {
		flow, err := BuildRoot(cfg, testRegistry(), pool.New(0, 0), nil, core.BlockDeps{})
		if err != nil {
			t.Fatalf("BuildRoot: %v", err)
		}
		// White-box, because the failure this guards against is a fallback to the
		// process-wide dispatcher, which no listener of our own could observe.
		if flow.events != nil {
			t.Error("a flow built with no dispatcher picked one up anyway")
		}
		if _, err = flow.Process(context.Background(), mustMessage(t)); err != nil {
			t.Fatalf("process: %v", err)
		}
	})

	t.Run("dispatcher wired", func(t *testing.T) {
		wired := core.NewBlockEvents()
		var paths []string
		// Sync, so the assertion needs no waiting: everything that is going to be
		// delivered has been by the time Process returns. Async delivery is the
		// dispatcher's business and is tested there.
		wired.AddSync(func(_ context.Context, ev types.BlockEvent) {
			if ev.Kind == types.BlockPreInvoke {
				paths = append(paths, ev.Path)
			}
		})

		flow, err := BuildRoot(cfg, testRegistry(), pool.New(0, 0), nil, core.BlockDeps{Events: wired})
		if err != nil {
			t.Fatalf("BuildRoot: %v", err)
		}
		if _, err = flow.Process(context.Background(), mustMessage(t)); err != nil {
			t.Fatalf("process: %v", err)
		}

		// The sub-flow inside the composite has to reach the same dispatcher: it is
		// built by a different builder, so it is its own chance to lose the wiring.
		wantTrace(t, paths, []string{"orders.lookup", "orders.lookup[body].inner"})
	})
}

// BenchmarkFlowProcessNoListeners is the number that answers "what does the hot
// loop pay for this": a flow with no dispatcher wired at all, which is every flow
// nobody is observing.
func BenchmarkFlowProcessNoListeners(b *testing.B) {
	flow, err := BuildRoot(
		types.FlowConfig{Name: "orders", Process: []types.BlockConfig{
			{Type: "pass", Name: "a"}, {Type: "pass", Name: "b"},
		}},
		testRegistry(), pool.New(0, 0), nil, core.BlockDeps{},
	)
	if err != nil {
		b.Fatalf("BuildRoot: %v", err)
	}

	ctx := context.Background()
	msg := mustMessage(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := flow.Process(ctx, msg); err != nil {
			b.Fatalf("process: %v", err)
		}
	}
}

// BenchmarkFlowProcessIdleDispatcher measures the other fast path: a dispatcher is
// wired (as it is for every flow a Service builds) but nothing has registered.
func BenchmarkFlowProcessIdleDispatcher(b *testing.B) {
	flow, err := BuildRoot(
		types.FlowConfig{Name: "orders", Process: []types.BlockConfig{
			{Type: "pass", Name: "a"}, {Type: "pass", Name: "b"},
		}},
		testRegistry(), pool.New(0, 0), nil, core.BlockDeps{Events: core.NewBlockEvents()},
	)
	if err != nil {
		b.Fatalf("BuildRoot: %v", err)
	}

	ctx := context.Background()
	msg := mustMessage(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := flow.Process(ctx, msg); err != nil {
			b.Fatalf("process: %v", err)
		}
	}
}
