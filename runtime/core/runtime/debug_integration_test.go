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
	return startDebugService(t, cfg, WithBreakpoint(bp))
}

// startDebugService runs svc in invoke mode with the given debug options and waits
// until it is ready, returning a cleanup the caller defers.
func startDebugService(t *testing.T, cfg types.Config, opts ...ServiceOption) (*Service, func()) {
	t.Helper()
	svc := NewService(cfg, core.DefaultRegistry(), append([]ServiceOption{WithInvokeMode()}, opts...)...)
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

// forkFlows is a fork with a block in each branch, then a block after the fork. It is
// the shape that proves the collector has to be out-of-band: a fork runs each branch
// on a clone and throws the branch's message away, so a spy inside one cannot report
// through the flow's result.
func forkFlows() types.Config {
	downstreamRuns.Store(0)

	stamp := func(name, stage string) types.BlockConfig {
		return types.BlockConfig{Type: "tbp.stamp", Name: name, Settings: types.Settings{"stage": stage}}
	}
	return types.Config{Flows: []types.FlowConfig{{
		Name: "orders",
		Process: []types.BlockConfig{
			{
				Type: "fork", Name: "fanout",
				Branches: []types.FlowConfig{
					{Name: "audit", Process: []types.BlockConfig{stamp("log-it", "audited")}},
					{Name: "notify", Process: []types.BlockConfig{stamp("send", "notified")}},
				},
			},
			// Named, because a block is addressed by its type only when it has no name —
			// and this one's type carries a dot, which the address grammar reads as a
			// step separator.
			{Type: "tbp.downstream", Name: "downstream"},
		},
	}}}
}

// TestSpyInsideAForkBranch is the end-to-end claim: a spy sees a block whose message
// the flow itself never returns. The fork clones the message into each branch and
// discards the result, so the only way to see what "log-it" received is the
// out-of-band collector — and the flow still runs to completion, unlike a breakpoint.
func TestSpyInsideAForkBranch(t *testing.T) {
	spies, err := core.NewSpies([]string{"orders.fanout[audit].log-it", "orders.downstream"})
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}

	svc, stop := startDebugService(t, forkFlows(), WithSpies(spies))
	defer stop()
	out := call(t, svc, "orders")

	// The spied block inside the branch was seen, with the message that branch built.
	branch := spies.Records("orders.fanout[audit].log-it")
	if len(branch) != 1 {
		t.Fatalf("got %d records inside the fork branch, want 1", len(branch))
	}
	if stage, _ := branch[0].Output.Variables.String("stage"); stage != "audited" {
		t.Errorf("branch record stage = %q, want %q — the spy must see the branch's own clone", stage, "audited")
	}

	// And the flow ran on regardless: the block after the fork still ran, and the
	// spied downstream block recorded the message the caller actually got back.
	if got := downstreamRuns.Load(); got != 1 {
		t.Errorf("the block after the fork ran %d times, want 1: a spy must not halt the flow", got)
	}
	if out.StopRequested() {
		t.Error("a spy must not ask the flow to stop")
	}
	if downstream := spies.Records("orders.downstream"); len(downstream) != 1 {
		t.Errorf("got %d records for the downstream block, want 1", len(downstream))
	}
}

// mockFlows is a straight chain: a block that stamps the message, then the block to
// mock. Straight, because a fork would not do — it runs each branch on a clone and
// throws it away, so nothing a branch stamps ever reaches the block after it, and the
// mock would have no input to dispatch on.
func mockFlows() types.Config {
	downstreamRuns.Store(0)

	return types.Config{Flows: []types.FlowConfig{{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "tbp.stamp", Name: "seed", Settings: types.Settings{"stage": "seeded"}},
			{Type: "tbp.downstream", Name: "downstream"},
		},
	}}}
}

// TestMockShortCircuitsTheRealBlock is the end-to-end claim mocking exists for: the
// block the mock replaced never runs. In a real flow that block is an HTTP call or an
// LLM, and "never runs" means no network and no spend — so this asserts on a counter
// the real block increments, not merely on the message coming back changed.
//
// The spy on the mocked block is the pairing that makes mocks useful: it shows what
// the flow fed the block, and what the mock answered in its place.
func TestMockShortCircuitsTheRealBlock(t *testing.T) {
	mocks, err := core.NewMocks(map[string]core.MockSpec{
		"orders.downstream": {Cases: []core.MockCase{
			{When: `vars.stage == "seeded"`, Body: map[string]any{"charged": true}},
			{When: "true", Error: "unexpected input"},
		}},
	})
	if err != nil {
		t.Fatalf("NewMocks: %v", err)
	}
	spies, err := core.NewSpies([]string{"orders.downstream"})
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}

	svc, stop := startDebugService(t, mockFlows(), WithMocks(mocks), WithSpies(spies))
	defer stop()
	out := call(t, svc, "orders")

	// The real block never ran — no network, no spend.
	if got := downstreamRuns.Load(); got != 0 {
		t.Errorf("the mocked block ran %d times, want 0: a mock must replace the block, not wrap it", got)
	}
	// And the flow carried the mock's answer to the end.
	body, ok := out.Body.(map[string]any)
	if !ok || body["charged"] != true {
		t.Errorf("flow result body = %v, want the mock's canned answer", out.Body)
	}

	// The spy wraps the mock, so it shows both sides: what the flow fed the block,
	// and what the mock answered.
	records := spies.Records("orders.downstream")
	if len(records) != 1 {
		t.Fatalf("got %d records for the mocked block, want 1", len(records))
	}
	if stage, _ := records[0].Input.Variables.String("stage"); stage != "seeded" {
		t.Errorf("spied input stage = %q, want the message the flow actually fed the block", stage)
	}
	if got := records[0].Output.Body.(map[string]any)["charged"]; got != true {
		t.Errorf("spied output = %v, want the mock's answer", records[0].Output.Body)
	}
}

// TestMockRequiresInvokeMode: a mock on a source-backed service would answer
// production traffic with a canned response.
func TestMockRequiresInvokeMode(t *testing.T) {
	mocks, err := core.NewMocks(map[string]core.MockSpec{
		"orders.downstream": {Cases: []core.MockCase{{When: "true", Drop: true}}},
	})
	if err != nil {
		t.Fatalf("NewMocks: %v", err)
	}

	svc := NewService(mockFlows(), core.DefaultRegistry(), WithMocks(mocks))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := svc.Run(ctx); err == nil {
		t.Fatal("a mock outside invoke mode must be refused")
	}
}

// TestSpyRequiresInvokeMode: nothing drains the collector outside an invoke, so on a
// source-backed flow a spy would hoard the body of every message that crossed the
// block, forever, for nobody to read.
func TestSpyRequiresInvokeMode(t *testing.T) {
	spies, err := core.NewSpies([]string{"orders.fanout[audit].log-it"})
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}

	svc := NewService(forkFlows(), core.DefaultRegistry(), WithSpies(spies))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := svc.Run(ctx); err == nil {
		t.Fatal("a spy outside invoke mode must be refused")
	}
}

// TestSpyBadAddressFailsTheRun: an unresolvable address is a mistake in the request,
// not an empty result — reporting "no records" would read like the block was never
// reached, which is exactly the wrong answer.
func TestSpyBadAddressFailsTheRun(t *testing.T) {
	spies, err := core.NewSpies([]string{"orders.nosuchblock"})
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}

	svc := NewService(forkFlows(), core.DefaultRegistry(), WithInvokeMode(), WithSpies(spies))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := svc.Run(ctx); err == nil {
		t.Fatal("an unresolvable spy address must fail the run")
	}
}
