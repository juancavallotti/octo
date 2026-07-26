package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/engine"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// slowBlockDelay is long enough that a measured duration is unambiguously
// non-zero on any clock, and short enough not to slow the suite down.
const slowBlockDelay = 5 * time.Millisecond

// eventTestBlocks extends the shared pass/drop/fail set with blocks that take
// measurable time, so a duration assertion is about the measurement and not the
// clock's resolution.
func eventTestBlocks() *core.BlockRegistry {
	reg := testBlocks()
	reg.MustRegister("slow-pass", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			time.Sleep(slowBlockDelay)
			return msg, nil
		}), nil
	})
	reg.MustRegister("slow-fail", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(context.Context, *types.Message) (*types.Message, error) {
			time.Sleep(slowBlockDelay)
			return nil, errTestBlockFailed
		}), nil
	})
	return reg
}

// errTestBlockFailed is the failure the slow-fail block returns.
var errTestBlockFailed = errTestBlock{}

type errTestBlock struct{}

func (errTestBlock) Error() string { return "boom" }

// newEventFlow builds a boundFlow from named root and (optional) error-path block
// configs, so a test can assert on the block label an event reports.
func newEventFlow(t *testing.T, root []types.BlockConfig, errorPath []types.BlockConfig, rec *recorder) *boundFlow {
	t.Helper()
	bus := core.NewEventBus()
	bus.Subscribe(rec.handle)
	p := pool.New(0, 0)
	reg := eventTestBlocks()

	rootFlow, err := engine.BuildRoot(
		types.FlowConfig{Name: "test", Process: root}, reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("build root: %v", err)
	}
	bf := &boundFlow{
		name:    "test",
		source:  fakeSource{},
		root:    rootFlow,
		workers: 1,
		in:      make(chan *types.Message, 8),
		bus:     bus,
		pool:    p,
	}
	if errorPath != nil {
		errFlow, buildErr := engine.BuildRoot(
			types.FlowConfig{Name: "test", Process: errorPath}, reg, p, nil, core.BlockDeps{},
		)
		if buildErr != nil {
			t.Fatalf("build error path: %v", buildErr)
		}
		bf.errorPath = errFlow
	}
	return bf
}

// started returns the single started event the recorder captured.
func (r *recorder) started(t *testing.T) types.FlowEvent {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Kind == types.FlowEventStarted {
			return e
		}
	}
	t.Fatal("no started event recorded")
	return types.FlowEvent{}
}

// Every terminal outcome carries how long the message took; the started event
// carries nothing, because nothing has happened yet.
func TestFlowEventDurationOnEveryTerminalOutcome(t *testing.T) {
	tests := []struct {
		name      string
		blockType string
		terminal  types.FlowEventKind
	}{
		{name: "completed", blockType: "slow-pass", terminal: types.FlowEventCompleted},
		{name: "failed", blockType: "slow-fail", terminal: types.FlowEventFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			bf := newEventFlow(t, []types.BlockConfig{{Type: tt.blockType}}, nil, rec)
			runOne(t, bf)

			if got := rec.started(t).Duration; got != 0 {
				t.Errorf("started event Duration = %v, want 0", got)
			}
			ev := rec.terminal(t)
			if ev.Kind != tt.terminal {
				t.Fatalf("terminal kind = %s, want %s", ev.Kind, tt.terminal)
			}
			if ev.Duration < slowBlockDelay {
				t.Errorf("Duration = %v, want at least the block's %v", ev.Duration, slowBlockDelay)
			}
		})
	}
}

// A dropped message still cost what it cost before the filter stopped it.
func TestFlowEventDurationOnDrop(t *testing.T) {
	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "slow-pass"}, {Type: "drop"}}, nil, rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventDropped {
		t.Fatalf("terminal kind = %s, want %s", ev.Kind, types.FlowEventDropped)
	}
	if ev.Duration < slowBlockDelay {
		t.Errorf("Duration = %v, want at least the block's %v", ev.Duration, slowBlockDelay)
	}
}

// The clock covers the error path: a message the error path recovered still cost
// what the error path spent, and a duration that stopped at the failure would
// under-report every flow that handles its errors.
func TestFlowEventDurationIncludesErrorPath(t *testing.T) {
	rec := &recorder{}
	bf := newEventFlow(t,
		[]types.BlockConfig{{Type: "fail"}},
		[]types.BlockConfig{{Type: "slow-pass"}},
		rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventCompleted {
		t.Fatalf("terminal kind = %s, want %s", ev.Kind, types.FlowEventCompleted)
	}
	if ev.Duration < slowBlockDelay {
		t.Errorf("Duration = %v, want at least the error path's %v", ev.Duration, slowBlockDelay)
	}
}

// The failing block's label is what turns "this flow is erroring" into "this
// block is erroring", and a name beats the type when one is given.
func TestFlowEventBlockNamesTheFailingBlock(t *testing.T) {
	tests := []struct {
		name  string
		root  []types.BlockConfig
		want  string
		kind  types.FlowEventKind
		named string
	}{
		{
			name: "named block",
			root: []types.BlockConfig{{Type: "pass"}, {Type: "fail", Name: "charge"}},
			want: "charge",
			kind: types.FlowEventFailed,
		},
		{
			name: "unnamed block falls back to its type",
			root: []types.BlockConfig{{Type: "fail"}},
			want: "fail",
			kind: types.FlowEventFailed,
		},
		{
			name: "completed carries no block",
			root: []types.BlockConfig{{Type: "pass"}},
			want: "",
			kind: types.FlowEventCompleted,
		},
		{
			name: "dropped carries no block",
			root: []types.BlockConfig{{Type: "drop"}},
			want: "",
			kind: types.FlowEventDropped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			bf := newEventFlow(t, tt.root, nil, rec)
			runOne(t, bf)

			ev := rec.terminal(t)
			if ev.Kind != tt.kind {
				t.Fatalf("terminal kind = %s, want %s", ev.Kind, tt.kind)
			}
			if ev.Block != tt.want {
				t.Errorf("Block = %q, want %q", ev.Block, tt.want)
			}
		})
	}
}

// The failed event means UNHANDLED: a flow whose error path recovered the message
// completed, so nothing counting failed events counts it. A flow erroring on every
// message but recovering must not read as failing.
func TestFlowEventRecoveredFailureIsNotReportedFailed(t *testing.T) {
	rec := &recorder{}
	bf := newEventFlow(t,
		[]types.BlockConfig{{Type: "fail", Name: "charge"}},
		[]types.BlockConfig{{Type: "pass"}},
		rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventCompleted {
		t.Fatalf("terminal kind = %s, want %s — a recovered failure completes", ev.Kind, types.FlowEventCompleted)
	}
	if ev.Block != "" {
		t.Errorf("Block = %q on a recovered failure, want empty", ev.Block)
	}
	if ev.Err != nil {
		t.Errorf("Err = %v on a recovered failure, want nil", ev.Err)
	}
}

// When the error path itself fails, the runtime re-wraps the error. The label must
// still survive the extra wrapping, and it must name the block that failed inside
// the error path — that is the one to go look at.
func TestFlowEventBlockSurvivesErrorPathWrapping(t *testing.T) {
	rec := &recorder{}
	bf := newEventFlow(t,
		[]types.BlockConfig{{Type: "fail", Name: "charge"}},
		[]types.BlockConfig{{Type: "fail", Name: "notify"}},
		rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventFailed {
		t.Fatalf("terminal kind = %s, want %s", ev.Kind, types.FlowEventFailed)
	}
	if ev.Block != "notify" {
		t.Errorf("Block = %q, want %q — the block that failed inside the error path", ev.Block, "notify")
	}
}

// A flow with no bus must not panic when publishing, and a flow with one must
// report a duration that is plausible rather than merely non-zero.
func TestFlowEventDurationIsBounded(t *testing.T) {
	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "pass"}}, nil, rec)

	before := time.Now()
	runOne(t, bf)
	wall := time.Since(before)

	ev := rec.terminal(t)
	if ev.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", ev.Duration)
	}
	if ev.Duration > wall {
		t.Errorf("Duration = %v exceeds the wall time of the whole run (%v)", ev.Duration, wall)
	}
}
