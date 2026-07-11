package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// stand returns the config for a mock standing in for the block at address, as the
// runtime's injector builds it: a leaf with the target's label and no sub-flows,
// because the block it replaced is gone from the tree.
func stand(address, label string) types.BlockConfig {
	return types.BlockConfig{
		Type:     blockKindMock,
		Name:     label,
		Settings: types.Settings{"address": address},
	}
}

func buildWithMocks(
	t *testing.T, reg *core.BlockRegistry, specs map[string]core.MockSpec, cfg types.FlowConfig,
) (*Flow, error) {
	t.Helper()
	mocks, err := core.NewMocks(specs)
	if err != nil {
		t.Fatalf("NewMocks: %v", err)
	}
	return (&builder{
		reg:  reg,
		pool: pool.New(0, 0),
		deps: core.BlockDeps{Mocks: mocks},
	}).flow(cfg)
}

func mustBuildWithMocks(
	t *testing.T, specs map[string]core.MockSpec, cfg types.FlowConfig,
) *Flow {
	t.Helper()
	flow, err := buildWithMocks(t, breakpointRegistry(new(bool)), specs, cfg)
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}
	return flow
}

// mockFlow is a one-block flow whose only block is a mock, which is what injection
// produces for a mocked leaf.
func mockFlow(spec core.MockSpec) (map[string]core.MockSpec, types.FlowConfig) {
	return map[string]core.MockSpec{"flow.charge": spec},
		types.FlowConfig{Process: []types.BlockConfig{stand("flow.charge", "charge")}}
}

// TestMockAppliesTheFirstMatchingCase: cases are tried in order, so a spec reads like
// the block's own logic — the specific case first, the catch-all last.
func TestMockAppliesTheFirstMatchingCase(t *testing.T) {
	specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{
		{When: `body.amount > 100`, Body: map[string]any{"status": "review"}},
		{When: `true`, Body: map[string]any{"status": "ok"}},
	}})
	flow := mustBuildWithMocks(t, specs, cfg)

	tests := []struct {
		amount int
		want   string
	}{
		{amount: 500, want: "review"},
		{amount: 5, want: "ok"}, // falls past the first case to the catch-all
	}

	for _, tc := range tests {
		msg := mustMessage(t)
		msg.Body = map[string]any{"amount": tc.amount}

		out, err := flow.Process(context.Background(), msg)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		body, ok := out.Body.(map[string]any)
		if !ok {
			t.Fatalf("body = %T, want a map", out.Body)
		}
		if body["status"] != tc.want {
			t.Errorf("amount %d: status = %v, want %q", tc.amount, body["status"], tc.want)
		}
	}
}

// TestMockAppliesVarsAlongsideTheBody: some blocks report through a variable rather
// than the body — an http call setting vars.status — so a mock of one has to be able
// to stand in for that too.
func TestMockAppliesVarsAlongsideTheBody(t *testing.T) {
	specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{{
		When: "true",
		Body: map[string]any{"id": 1},
		Vars: map[string]any{"status": 200},
	}}})
	flow := mustBuildWithMocks(t, specs, cfg)

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if status, _ := out.Variables.Int("status"); status != 200 {
		t.Errorf("vars.status = %v, want 200", out.Variables["status"])
	}
}

// TestMockBodyIsNotSharedBetweenMessages is why the canned body is encoded once and
// decoded per message. Two messages through the same mock get their own copy, so a
// downstream block mutating one message's body cannot rewrite the mock's answer for
// the next — which, with a mock shared across every worker in a flow, would be a data
// race as well as a wrong answer.
func TestMockBodyIsNotSharedBetweenMessages(t *testing.T) {
	specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{
		{When: "true", Body: map[string]any{"status": "ok"}},
	}})
	flow := mustBuildWithMocks(t, specs, cfg)

	first, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// A downstream block mutates the body it was handed.
	first.Body.(map[string]any)["status"] = "tampered"

	second, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := second.Body.(map[string]any)["status"]; got != "ok" {
		t.Errorf("the second message got status %q: the mock body must not be shared", got)
	}
}

// TestMockOutcomes covers the two outcomes that are not a message. They are what make
// an error path or a filter testable without arranging for the real block to fail.
func TestMockOutcomes(t *testing.T) {
	t.Run("error fails the block with the target's label", func(t *testing.T) {
		specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{
			{When: "true", Error: "card declined"},
		}})
		flow := mustBuildWithMocks(t, specs, cfg)

		_, err := flow.Process(context.Background(), mustMessage(t))
		if err == nil {
			t.Fatal("an error case must fail the block")
		}
		// The mock carries the target's label, so the failure is indistinguishable
		// from the real block failing — and vars.error.block still names the target.
		var blockErr *blockError
		if !errors.As(err, &blockErr) {
			t.Fatalf("error is not a *blockError: %v", err)
		}
		if blockErr.label != "charge" {
			t.Errorf("vars.error.block would report %q, want %q", blockErr.label, "charge")
		}
		if err.Error() != `block "charge": card declined` {
			t.Errorf("error = %q, want it to read like the real block failing", err)
		}
	})

	t.Run("drop filters the message out", func(t *testing.T) {
		specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{{When: "true", Drop: true}}})
		flow := mustBuildWithMocks(t, specs, cfg)

		out, err := flow.Process(context.Background(), mustMessage(t))
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if out != nil {
			t.Error("a drop case must drop the message")
		}
	})
}

// TestMockFallsBackToTheDefault: the default is the catch-all for a message no case
// matched.
func TestMockFallsBackToTheDefault(t *testing.T) {
	specs, cfg := mockFlow(core.MockSpec{
		Cases:   []core.MockCase{{When: `body.amount > 100`, Error: "declined"}},
		Default: &core.MockCase{Body: map[string]any{"status": "ok"}},
	})
	flow := mustBuildWithMocks(t, specs, cfg)

	msg := mustMessage(t)
	msg.Body = map[string]any{"amount": 5}

	out, err := flow.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := out.Body.(map[string]any)["status"]; got != "ok" {
		t.Errorf("status = %v, want the default to have applied", got)
	}
}

// TestMockWithNoMatchAndNoDefaultFails is the deliberate choice at the heart of the
// mock: it does NOT fall through to the real block, because the real block is gone
// from the tree. Keeping one wired would mean a "mocked" HTTP or LLM call could still
// reach the network on a run the user believes is mocked. An unmatched message is a
// hole in the spec, and saying so beats silently doing nothing.
func TestMockWithNoMatchAndNoDefaultFails(t *testing.T) {
	specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{
		{When: `body.amount > 100`, Body: map[string]any{"status": "review"}},
	}})
	flow := mustBuildWithMocks(t, specs, cfg)

	msg := mustMessage(t)
	msg.Body = map[string]any{"amount": 5}

	_, err := flow.Process(context.Background(), msg)
	if err == nil {
		t.Fatal("a message no case matched must fail, not pass through to the block the mock replaced")
	}
	if got := err.Error(); !strings.Contains(got, "no mock case matched") || !strings.Contains(got, "default") {
		t.Errorf("error = %q, want it to say nothing matched and how to fix it", got)
	}
}

// TestMockConditionIsCompiledAtBuildTime: a mock that cannot compile must fail the
// run, not the first message that happens to reach it.
func TestMockConditionIsCompiledAtBuildTime(t *testing.T) {
	specs, cfg := mockFlow(core.MockSpec{Cases: []core.MockCase{
		{When: `body.amount >`, Body: map[string]any{}},
	}})

	if _, err := buildWithMocks(t, breakpointRegistry(new(bool)), specs, cfg); err == nil {
		t.Fatal("a mock whose condition does not compile must fail the build")
	}
}

// TestMockCannotBeAuthored: the block is injected by --mocks, never written in a
// flow. Without the specs wired it refuses to build, so a config that declares it by
// hand fails loudly.
func TestMockCannotBeAuthored(t *testing.T) {
	_, err := (&builder{reg: breakpointRegistry(new(bool)), pool: pool.New(0, 0)}).flow(types.FlowConfig{
		Process: []types.BlockConfig{{Type: blockKindMock, Settings: types.Settings{"address": "flow.charge"}}},
	})
	if err == nil {
		t.Fatal("a hand-authored mock block must fail to build")
	}
	if !errors.Is(err, errMockUnwired) {
		t.Errorf("error = %v, want errMockUnwired", err)
	}
}
