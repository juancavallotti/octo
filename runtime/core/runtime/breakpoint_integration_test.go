package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// startBreakService runs svc in invoke mode with a breakpoint and waits until it is
// ready, returning a cleanup the caller defers.
func startBreakService(t *testing.T, cfg types.Config, bp *core.Breakpoint) (*Service, func()) {
	t.Helper()
	svc := NewService(cfg, core.DefaultRegistry(), WithInvokeMode(), WithBreakpoint(bp))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	select {
	case <-svc.Started():
	case err := <-done:
		t.Fatalf("service stopped before ready: %v", err)
	case <-time.After(e2eWaitTimeout):
		cancel()
		t.Fatal("service did not become ready")
	}
	return svc, func() {
		cancel()
		<-done
	}
}

// call invokes the flow and returns its result.
func call(t *testing.T, svc *Service, flow string) *types.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := svc.Flows().Call(ctx, flow, mustMessage(t))
	if err != nil {
		t.Fatalf("Call(%q): %v", flow, err)
	}
	return out
}

// The block registry is process-wide, so the test blocks register once. Each counts
// into the atomic the running test installs, letting a test prove a block did or did
// not run rather than only inspecting the message.
var (
	downstreamRuns atomic.Int64
	flowRefRuns    atomic.Int64
)

func init() {
	core.MustRegisterBlock("tbp.stamp", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		stage, _ := s.String("stage")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Variables.Set("stage", stage)
			return msg, nil
		}), nil
	})
	core.MustRegisterBlock("tbp.downstream", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			downstreamRuns.Add(1)
			msg.Variables.Set("stage", "downstream")
			return msg, nil
		}), nil
	})
	core.MustRegisterBlock("tbp.sub", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Variables.Set("stage", "inside-sub")
			return msg, nil
		}), nil
	})
	core.MustRegisterBlock("tbp.after", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			flowRefRuns.Add(1)
			return msg, nil
		}), nil
	})
}

// breakFlows is a flow with an if/else, a block in each branch, and a block after
// the if — the shape that shows both "reached" and "never reached". The downstream
// block counts its runs, so a test can prove the flow really halted instead of just
// carrying the flag.
func breakFlows(condition string) types.Config {
	downstreamRuns.Store(0)

	stamp := func(name, stage string) types.BlockConfig {
		return types.BlockConfig{Type: "tbp.stamp", Name: name, Settings: types.Settings{"stage": stage}}
	}
	return types.Config{Flows: []types.FlowConfig{{
		Name: "orders",
		Process: []types.BlockConfig{
			{
				Type: "if", Name: "checkHeader", Condition: condition,
				Then: &types.FlowConfig{Process: []types.BlockConfig{stamp("charge", "then")}},
				Else: &types.FlowConfig{Process: []types.BlockConfig{stamp("api-call-1", "else")}},
			},
			{Type: "tbp.downstream"},
		},
	}}}
}

// TestBreakpointHitInTakenBranch: the message enters the branch the breakpoint is
// in, so the snapshot is recorded and the block after the if never runs.
func TestBreakpointHitInTakenBranch(t *testing.T) {
	bp := core.NewBreakpoint("orders.checkHeader[else].api-call-1")

	svc, stop := startBreakService(t, breakFlows("false"), bp)
	defer stop()
	call(t, svc, "orders")

	snapshot, ok := bp.Snapshot()
	if !ok {
		t.Fatal("breakpoint reports not reached, but the else branch was taken")
	}
	if stage, _ := snapshot.Variables.String("stage"); stage != "else" {
		t.Errorf("snapshot stage = %q, want %q: the snapshot must be the message after the target ran", stage, "else")
	}
	if snapshot.StopRequested() {
		t.Error("the snapshot must not carry the internal stop flag")
	}
	if got := downstreamRuns.Load(); got != 0 {
		t.Errorf("the block after the if ran %d times, want 0: the breakpoint must halt the flow", got)
	}
}

// TestBreakpointNotReachedInBranchNotTaken is the case the feature exists for: the
// message takes the other branch, so there is nothing to report — and the flow runs
// to completion exactly as it would have without the breakpoint.
func TestBreakpointNotReachedInBranchNotTaken(t *testing.T) {
	bp := core.NewBreakpoint("orders.checkHeader[else].api-call-1")

	// The condition is true, so the message takes "then" and never sees the target.
	svc, stop := startBreakService(t, breakFlows("true"), bp)
	defer stop()
	out := call(t, svc, "orders")

	if _, ok := bp.Snapshot(); ok {
		t.Error("breakpoint recorded a snapshot, but its branch was never taken")
	}
	if got := downstreamRuns.Load(); got != 1 {
		t.Errorf("the block after the if ran %d times, want 1: an unreached breakpoint must not halt the flow", got)
	}
	if stage, _ := out.Variables.String("stage"); stage != "downstream" {
		t.Errorf("flow result stage = %q, want the flow to have run to completion", stage)
	}
}

// TestBreakpointAcrossFlowRef: the address names a flow other than the invoked one.
// The breakpoint fires inside the called flow, and the stop folds back through the
// flow-ref so the caller halts too.
func TestBreakpointAcrossFlowRef(t *testing.T) {
	flowRefRuns.Store(0)

	cfg := types.Config{Flows: []types.FlowConfig{
		{Name: "caller", Process: []types.BlockConfig{
			{Type: "flow-ref", Settings: types.Settings{"flow": "sub"}},
			{Type: "tbp.after"},
		}},
		{Name: "sub", Process: []types.BlockConfig{{Type: "tbp.sub", Name: "step"}}},
	}}

	bp := core.NewBreakpoint("sub.step")
	svc, stop := startBreakService(t, cfg, bp)
	defer stop()
	call(t, svc, "caller")

	snapshot, ok := bp.Snapshot()
	if !ok {
		t.Fatal("breakpoint in a flow-ref'd flow was not reached")
	}
	if stage, _ := snapshot.Variables.String("stage"); stage != "inside-sub" {
		t.Errorf("snapshot stage = %q, want %q", stage, "inside-sub")
	}
	if got := flowRefRuns.Load(); got != 0 {
		t.Errorf("the block after the flow-ref ran %d times, want 0: the stop must fold back to the caller", got)
	}
}

// TestBreakpointRequiresInvokeMode: a breakpoint on a source-backed service would
// halt whichever production message arrived first, so it is refused.
func TestBreakpointRequiresInvokeMode(t *testing.T) {
	svc := NewService(breakFlows("true"), core.DefaultRegistry(),
		WithBreakpoint(core.NewBreakpoint("orders.checkHeader[then].charge")))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := svc.Run(ctx); err == nil {
		t.Fatal("a breakpoint outside invoke mode must be refused")
	}
}

// TestBreakpointBadAddressFailsTheRun: an address that resolves to nothing is a
// mistake in the request, not a "never reached" result — it must fail loudly rather
// than run the flow and report nothing found.
func TestBreakpointBadAddressFailsTheRun(t *testing.T) {
	svc := NewService(breakFlows("true"), core.DefaultRegistry(), WithInvokeMode(),
		WithBreakpoint(core.NewBreakpoint("orders.nosuchblock")))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := svc.Run(ctx); err == nil {
		t.Fatal("an unresolvable breakpoint address must fail the run")
	}
}
