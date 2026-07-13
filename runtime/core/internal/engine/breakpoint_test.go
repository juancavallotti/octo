package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// breakpointRegistry returns a registry with the leaves the breakpoint tests wrap:
// "mark" stamps a variable and appends to the body so a snapshot can be told apart
// from the state before and after it; "record" flips a flag when it runs; "boom"
// fails; "drop" filters the message out.
func breakpointRegistry(ran *bool) *core.BlockRegistry {
	reg := core.NewBlockRegistry()
	reg.MustRegister("mark", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		value, _ := s.String("value")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Variables.Set("stage", value)
			return msg, nil
		}), nil
	})
	reg.MustRegister("record", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			*ran = true
			msg.Variables.Set("stage", "after")
			return msg, nil
		}), nil
	})
	reg.MustRegister("boom", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, _ *types.Message) (*types.Message, error) {
			return nil, errors.New("boom")
		}), nil
	})
	reg.MustRegister("drop", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, _ *types.Message) (*types.Message, error) {
			return nil, nil
		}), nil
	})
	return reg
}

// wrap returns the config for a breakpoint wrapping target, as the runtime's
// injector builds it: the wrapper takes the target's label so error reporting is
// unchanged.
func wrap(label string, target types.BlockConfig) types.BlockConfig {
	return types.BlockConfig{
		Type:    blockKindBreakpoint,
		Name:    label,
		Process: []types.BlockConfig{target},
	}
}

func buildWithBreakpoint(
	t *testing.T, reg *core.BlockRegistry, bp *core.Breakpoint, cfg types.FlowConfig,
) *Flow {
	t.Helper()
	flow, err := (&builder{
		reg:  reg,
		pool: pool.New(0, 0),
		deps: core.BlockDeps{Breakpoint: bp},
	}).flow(cfg)
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}
	return flow
}

// TestBreakpointRecordsAfterTargetAndHalts is the core contract: the snapshot is
// the message as it stood once the wrapped block ran, and nothing after it runs.
func TestBreakpointRecordsAfterTargetAndHalts(t *testing.T) {
	var ran bool
	bp := core.NewBreakpoint("flow.target")

	// mark(before) -> [breakpoint: mark(target)] -> record
	flow := buildWithBreakpoint(t, breakpointRegistry(&ran), bp, types.FlowConfig{
		Process: []types.BlockConfig{
			{Type: "mark", Settings: types.Settings{"value": "before"}},
			wrap("target", types.BlockConfig{Type: "mark", Settings: types.Settings{"value": "target"}}),
			{Type: "record"},
		},
	})

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("a breakpoint must halt the flow")
	}
	if ran {
		t.Error("the block after a breakpoint must not run")
	}

	snapshot, ok := bp.Snapshot()
	if !ok {
		t.Fatal("breakpoint reports not reached, want a snapshot")
	}
	// The snapshot is the state after the target ran, not before it and not after
	// the block that follows it.
	if stage, _ := snapshot.Variables.String("stage"); stage != "target" {
		t.Errorf("snapshot stage = %q, want %q", stage, "target")
	}
}

// TestBreakpointSnapshotOmitsStopFlag pins the record-then-stop order: the stop
// flag is bookkeeping and must not leak into the message the user is shown.
func TestBreakpointSnapshotOmitsStopFlag(t *testing.T) {
	bp := core.NewBreakpoint("flow.target")
	flow := buildWithBreakpoint(t, breakpointRegistry(new(bool)), bp, types.FlowConfig{
		Process: []types.BlockConfig{
			wrap("target", types.BlockConfig{Type: "mark", Settings: types.Settings{"value": "target"}}),
		},
	})

	if _, err := flow.Process(context.Background(), mustMessage(t)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	snapshot, ok := bp.Snapshot()
	if !ok {
		t.Fatal("breakpoint reports not reached, want a snapshot")
	}
	if snapshot.StopRequested() {
		t.Error("the snapshot must not carry the stop flag: record happens before RequestStop")
	}
}

// TestBreakpointSnapshotIsIsolated pins that the snapshot is a clone: the live
// message keeps travelling after the stop (an enrich folds its result back into
// it), and that must not rewrite what the user is shown.
func TestBreakpointSnapshotIsIsolated(t *testing.T) {
	bp := core.NewBreakpoint("flow.target")
	flow := buildWithBreakpoint(t, breakpointRegistry(new(bool)), bp, types.FlowConfig{
		Process: []types.BlockConfig{
			wrap("target", types.BlockConfig{Type: "mark", Settings: types.Settings{"value": "target"}}),
		},
	})

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	out.Variables.Set("stage", "mutated-after-the-fact")

	snapshot, _ := bp.Snapshot()
	if stage, _ := snapshot.Variables.String("stage"); stage != "target" {
		t.Errorf("snapshot stage = %q, want %q: the snapshot must not alias the live message", stage, "target")
	}
}

// TestBreakpointNotReachedWhenBranchNotTaken is the case the feature exists for:
// the block sits in a branch the message never enters, so there is nothing to show
// and the flow completes normally.
func TestBreakpointNotReachedWhenBranchNotTaken(t *testing.T) {
	var ran bool
	bp := core.NewBreakpoint("flow.checkHeader[then].target")

	// if(false) -> then{ [breakpoint: mark] }, else nothing; then a record block.
	flow := buildWithBreakpoint(t, breakpointRegistry(&ran), bp, types.FlowConfig{
		Process: []types.BlockConfig{
			{
				Type:      "if",
				Name:      "checkHeader",
				Condition: "false",
				Then: &types.FlowConfig{Process: []types.BlockConfig{
					wrap("target", types.BlockConfig{Type: "mark", Settings: types.Settings{"value": "target"}}),
				}},
			},
			{Type: "record"},
		},
	})

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || out.StopRequested() {
		t.Fatal("an unreached breakpoint must not halt the flow")
	}
	if !ran {
		t.Error("the flow must run to completion when the breakpoint is never reached")
	}
	if _, ok := bp.Snapshot(); ok {
		t.Error("breakpoint reports a snapshot, want not reached")
	}
}

// TestBreakpointOnFailingTargetIsNotReached: a block that fails produced no
// message at that point, so there is nothing to snapshot and the error stands.
func TestBreakpointOnFailingTargetIsNotReached(t *testing.T) {
	bp := core.NewBreakpoint("flow.target")
	flow := buildWithBreakpoint(t, breakpointRegistry(new(bool)), bp, types.FlowConfig{
		Process: []types.BlockConfig{wrap("boom", types.BlockConfig{Type: "boom"})},
	})

	if _, err := flow.Process(context.Background(), mustMessage(t)); err == nil {
		t.Fatal("a failing target must still fail the flow")
	}
	if _, ok := bp.Snapshot(); ok {
		t.Error("breakpoint recorded a snapshot for a block that failed")
	}
}

// TestBreakpointOnDroppingTargetIsNotReached: a block that filters the message out
// leaves no message to show, and the drop still propagates.
func TestBreakpointOnDroppingTargetIsNotReached(t *testing.T) {
	bp := core.NewBreakpoint("flow.target")
	flow := buildWithBreakpoint(t, breakpointRegistry(new(bool)), bp, types.FlowConfig{
		Process: []types.BlockConfig{wrap("drop", types.BlockConfig{Type: "drop"})},
	})

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != nil {
		t.Error("a dropping target must still drop the message")
	}
	if _, ok := bp.Snapshot(); ok {
		t.Error("breakpoint recorded a snapshot for a block that dropped the message")
	}
}

// TestBreakpointPreservesErrorLabel is the "do not change the program you are
// debugging" guarantee. A block error is labelled with the block's name, and an
// error path can branch on vars.error.block — so wrapping a block must not rename
// it, nor add a layer of its own. The wrapper takes the target's label and strips
// the label its own sub-flow adds; this pins the result against the unwrapped flow,
// for a leaf and for a composite (which nests labels on its own, so it also catches
// stripping one layer too many).
func TestBreakpointPreservesErrorLabel(t *testing.T) {
	tests := []struct {
		name      string
		target    types.BlockConfig
		wantLabel string
	}{
		{
			name:      "leaf",
			target:    types.BlockConfig{Type: "boom", Name: "charge-card"},
			wantLabel: "charge-card",
		},
		{
			name: "composite",
			target: types.BlockConfig{
				Type: "if", Name: "checkHeader", Condition: "true",
				Then: &types.FlowConfig{Process: []types.BlockConfig{{Type: "boom", Name: "inner"}}},
			},
			wantLabel: "checkHeader",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := breakpointRegistry(new(bool))

			plain, err := (&builder{reg: reg, pool: pool.New(0, 0)}).flow(types.FlowConfig{
				Process: []types.BlockConfig{tc.target},
			})
			if err != nil {
				t.Fatalf("build plain flow: %v", err)
			}
			_, plainErr := plain.Process(context.Background(), mustMessage(t))

			broken := buildWithBreakpoint(t, reg, core.NewBreakpoint("flow."+tc.wantLabel), types.FlowConfig{
				Process: []types.BlockConfig{wrap(tc.wantLabel, tc.target)},
			})
			_, brokenErr := broken.Process(context.Background(), mustMessage(t))

			if plainErr == nil || brokenErr == nil {
				t.Fatal("both flows must fail")
			}
			if plainErr.Error() != brokenErr.Error() {
				t.Errorf("breakpoint changed the error:\n without = %v\n with    = %v", plainErr, brokenErr)
			}

			var blockErr *blockError
			if !errors.As(brokenErr, &blockErr) {
				t.Fatal("wrapped error is not a *blockError")
			}
			if blockErr.label != tc.wantLabel {
				t.Errorf("vars.error.block would report %q, want %q", blockErr.label, tc.wantLabel)
			}
		})
	}
}

// TestBreakpointCannotBeAuthored: the block is injected by --break-at, never
// written in a flow. Without a collector wired it refuses to build, so a config
// that declares it by hand fails loudly.
func TestBreakpointCannotBeAuthored(t *testing.T) {
	_, err := (&builder{reg: breakpointRegistry(new(bool)), pool: pool.New(0, 0)}).flow(types.FlowConfig{
		Process: []types.BlockConfig{
			{Type: blockKindBreakpoint, Process: []types.BlockConfig{{Type: "mark"}}},
		},
	})
	if err == nil {
		t.Fatal("a hand-authored breakpoint block must fail to build")
	}
	if !errors.Is(err, errBreakpointUnwired) {
		t.Errorf("error = %v, want errBreakpointUnwired", err)
	}
}
