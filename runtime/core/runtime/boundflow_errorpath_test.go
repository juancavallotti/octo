package runtime

import (
	"context"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/internal/engine"
	"github.com/juancavallotti/eip-go/core/internal/pool"
	"github.com/juancavallotti/eip-go/types"
)

// newErrorPathFlow builds a boundFlow whose root runs rootType and whose
// flow-level error path runs errType.
func newErrorPathFlow(t *testing.T, rootType, errType string, rec *recorder) *boundFlow {
	t.Helper()
	bus := core.NewEventBus()
	bus.Subscribe(rec.handle)
	p := pool.New(0, 0)
	reg := testBlocks()

	root, err := engine.BuildRoot(
		types.FlowConfig{Process: []types.BlockConfig{{Type: rootType}}},
		reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("build root: %v", err)
	}
	errorPath, err := engine.BuildRoot(
		types.FlowConfig{Name: "test", Process: []types.BlockConfig{{Type: errType}}},
		reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("build error path: %v", err)
	}
	return &boundFlow{
		name:      "test",
		source:    fakeSource{},
		root:      root,
		errorPath: errorPath,
		workers:   1,
		in:        make(chan *types.Message, 8),
		bus:       bus,
		pool:      p,
	}
}

// terminal returns the single non-started event the recorder captured.
func (r *recorder) terminal(t *testing.T) types.FlowEvent {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Kind != types.FlowEventStarted {
			return e
		}
	}
	t.Fatal("no terminal event recorded")
	return types.FlowEvent{}
}

func runOne(t *testing.T, bf *boundFlow) {
	t.Helper()
	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	bf.in <- mustMessage(t)
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestFlowErrorPathRecovers(t *testing.T) {
	rec := &recorder{}
	bf := newErrorPathFlow(t, "fail", "pass", rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventCompleted {
		t.Fatalf("terminal kind = %s, want %s", ev.Kind, types.FlowEventCompleted)
	}
	if ev.Result == nil {
		t.Fatal("completed event has no result")
	}
	raw, ok := ev.Result.Variables["error"]
	if !ok {
		t.Fatal("vars.error not exposed to the error path")
	}
	e, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("vars.error is %T, want map[string]any", raw)
	}
	if e["flow"] != "test" {
		t.Errorf("vars.error.flow = %v, want %q", e["flow"], "test")
	}
	if e["block"] != "fail" {
		t.Errorf("vars.error.block = %v, want %q", e["block"], "fail")
	}
	if msg, _ := e["message"].(string); msg == "" {
		t.Error("vars.error.message is empty")
	}
}

func TestFlowErrorPathFailureReportsFailed(t *testing.T) {
	rec := &recorder{}
	bf := newErrorPathFlow(t, "fail", "fail", rec)
	runOne(t, bf)

	if k := rec.terminal(t).Kind; k != types.FlowEventFailed {
		t.Errorf("terminal kind = %s, want %s", k, types.FlowEventFailed)
	}
}

func TestFlowErrorPathDropReportsDropped(t *testing.T) {
	rec := &recorder{}
	bf := newErrorPathFlow(t, "fail", "drop", rec)
	runOne(t, bf)

	if k := rec.terminal(t).Kind; k != types.FlowEventDropped {
		t.Errorf("terminal kind = %s, want %s", k, types.FlowEventDropped)
	}
}

func TestFlowErrorPathSkippedOnSuccess(t *testing.T) {
	rec := &recorder{}
	bf := newErrorPathFlow(t, "pass", "fail", rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventCompleted {
		t.Fatalf("terminal kind = %s, want %s", ev.Kind, types.FlowEventCompleted)
	}
	if _, ok := ev.Result.Variables["error"]; ok {
		t.Error("vars.error set on the happy path; error path must not run")
	}
}
