package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// watch returns the config for a spy wrapping target, as the runtime's injector
// builds it: the wrapper takes the target's label, and carries the address the
// records file under.
func watch(address string, target types.BlockConfig) types.BlockConfig {
	return types.BlockConfig{
		Type:     blockKindSpy,
		Name:     blockLabel(target.Type, target.Name),
		Settings: types.Settings{"address": address},
		Process:  []types.BlockConfig{target},
	}
}

func buildWithSpies(t *testing.T, reg *core.BlockRegistry, spies *core.Spies, cfg types.FlowConfig) *Flow {
	t.Helper()
	flow, err := (&builder{
		reg:  reg,
		pool: pool.New(0, 0),
		deps: core.BlockDeps{Spies: spies},
	}).flow(cfg)
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}
	return flow
}

func mustSpies(t *testing.T, addresses ...string) *core.Spies {
	t.Helper()
	spies, err := core.NewSpies(addresses)
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}
	return spies
}

// TestSpyRecordsInputAndOutput is the core contract, and the reason the input is
// cloned before the block runs: "mark" mutates the message in place and hands the
// same pointer back, so a record taken afterwards would show the input already
// overwritten by the output.
func TestSpyRecordsInputAndOutput(t *testing.T) {
	spies := mustSpies(t, "flow.target")
	var ran bool

	// mark(before) -> [spy: mark(target)] -> record
	flow := buildWithSpies(t, breakpointRegistry(&ran), spies, types.FlowConfig{
		Process: []types.BlockConfig{
			{Type: "mark", Settings: types.Settings{"value": "before"}},
			watch("flow.target", types.BlockConfig{
				Type: "mark", Name: "target", Settings: types.Settings{"value": "target"},
			}),
			{Type: "record"},
		},
	})

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Unlike a breakpoint, a spy does not change the run: the flow completes and the
	// block after the spied one still runs.
	if out == nil || out.StopRequested() {
		t.Fatal("a spy must not halt the flow")
	}
	if !ran {
		t.Error("the block after a spy must still run")
	}

	records := spies.Records("flow.target")
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	rec := records[0]
	if stage, _ := rec.Input.Variables.String("stage"); stage != "before" {
		t.Errorf("input stage = %q, want %q: the input must be the message as the block received it", stage, "before")
	}
	if rec.Output == nil {
		t.Fatal("record has no output")
	}
	if stage, _ := rec.Output.Variables.String("stage"); stage != "target" {
		t.Errorf("output stage = %q, want %q", stage, "target")
	}
	if rec.Dropped || rec.Error != "" {
		t.Errorf("a block that returned a message must not record a drop or an error: %+v", rec)
	}
}

// TestSpyRecordsAreIsolated pins that both sides of a record are clones: the live
// message travels on after the spy, and a later block rewriting it must not rewrite
// what the user is shown.
func TestSpyRecordsAreIsolated(t *testing.T) {
	spies := mustSpies(t, "flow.target")

	// [spy: mark(target)] -> record, which sets stage to "after" on the live message.
	flow := buildWithSpies(t, breakpointRegistry(new(bool)), spies, types.FlowConfig{
		Process: []types.BlockConfig{
			watch("flow.target", types.BlockConfig{
				Type: "mark", Name: "target", Settings: types.Settings{"value": "target"},
			}),
			{Type: "record"},
		},
	})

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	out.Variables.Set("stage", "mutated-after-the-fact")

	rec := spies.Records("flow.target")[0]
	if stage, _ := rec.Output.Variables.String("stage"); stage != "target" {
		t.Errorf("output stage = %q, want %q: the record must not alias the live message", stage, "target")
	}
}

// TestSpyRecordsDropAndError: the two outcomes that are not a message are the ones a
// spy exists to explain — a block that filtered the message out, and a block that
// failed. Both are still propagated unchanged.
func TestSpyRecordsDropAndError(t *testing.T) {
	tests := []struct {
		name       string
		target     types.BlockConfig
		wantDrop   bool
		wantErr    string
		wantFlowNo bool // the flow must still fail
	}{
		{
			name:     "drop",
			target:   types.BlockConfig{Type: "drop", Name: "filter"},
			wantDrop: true,
		},
		{
			name:       "error",
			target:     types.BlockConfig{Type: "boom", Name: "charge"},
			wantErr:    "boom",
			wantFlowNo: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spies := mustSpies(t, "flow.target")
			flow := buildWithSpies(t, breakpointRegistry(new(bool)), spies, types.FlowConfig{
				Process: []types.BlockConfig{watch("flow.target", tc.target)},
			})

			out, err := flow.Process(context.Background(), mustMessage(t))
			switch {
			case tc.wantFlowNo && err == nil:
				t.Fatal("a failing target must still fail the flow")
			case !tc.wantFlowNo && err != nil:
				t.Fatalf("Process: %v", err)
			case !tc.wantFlowNo && out != nil:
				t.Error("a dropping target must still drop the message")
			}

			records := spies.Records("flow.target")
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1: a drop and a failure are still crossings", len(records))
			}
			rec := records[0]
			if rec.Input == nil {
				t.Error("even a drop or a failure records what went in")
			}
			if rec.Output != nil {
				t.Error("a block that produced no message must record no output")
			}
			if rec.Dropped != tc.wantDrop {
				t.Errorf("Dropped = %v, want %v", rec.Dropped, tc.wantDrop)
			}
			if rec.Error != tc.wantErr {
				t.Errorf("Error = %q, want %q", rec.Error, tc.wantErr)
			}
		})
	}
}

// TestSpyRecordsEveryCrossing: a block inside a foreach body runs once per element.
// That is why the collector appends rather than keeping the first write like a
// breakpoint — a spy that showed only the first iteration would hide the one that
// went wrong.
func TestSpyRecordsEveryCrossing(t *testing.T) {
	spies := mustSpies(t, "flow.loop[body].target")

	flow := buildWithSpies(t, breakpointRegistry(new(bool)), spies, types.FlowConfig{
		Process: []types.BlockConfig{{
			Type: "foreach", Name: "loop", Items: "body.items",
			Body: &types.FlowConfig{Process: []types.BlockConfig{
				watch("flow.loop[body].target", types.BlockConfig{
					Type: "mark", Name: "target", Settings: types.Settings{"value": "target"},
				}),
			}},
		}},
	})

	msg := mustMessage(t)
	msg.Body = map[string]any{"items": []any{"a", "b", "c"}}
	if _, err := flow.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	records := spies.Records("flow.loop[body].target")
	if len(records) != 3 {
		t.Fatalf("got %d records, want one per element", len(records))
	}
	// Records for one address come back in the order they were taken.
	for i := 1; i < len(records); i++ {
		if records[i].Seq <= records[i-1].Seq {
			t.Errorf("record %d has seq %d, not after %d", i, records[i].Seq, records[i-1].Seq)
		}
	}
}

// TestSpyPreservesErrorLabel is the "do not change the program you are debugging"
// guarantee, the same one the breakpoint keeps: a block error is labelled with the
// block's name and an error path can branch on vars.error.block, so wrapping a block
// must not rename it nor add a layer of its own.
func TestSpyPreservesErrorLabel(t *testing.T) {
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

			watched := buildWithSpies(t, reg, mustSpies(t, "flow."+tc.wantLabel), types.FlowConfig{
				Process: []types.BlockConfig{watch("flow."+tc.wantLabel, tc.target)},
			})
			_, watchedErr := watched.Process(context.Background(), mustMessage(t))

			if plainErr == nil || watchedErr == nil {
				t.Fatal("both flows must fail")
			}
			if plainErr.Error() != watchedErr.Error() {
				t.Errorf("the spy changed the error:\n without = %v\n with    = %v", plainErr, watchedErr)
			}

			var blockErr *blockError
			if !errors.As(watchedErr, &blockErr) {
				t.Fatal("wrapped error is not a *blockError")
			}
			if blockErr.label != tc.wantLabel {
				t.Errorf("vars.error.block would report %q, want %q", blockErr.label, tc.wantLabel)
			}
		})
	}
}

// TestSpyCannotBeAuthored: the block is injected by --spies, never written in a
// flow. Without a collector wired it refuses to build, so a config that declares it
// by hand fails loudly.
func TestSpyCannotBeAuthored(t *testing.T) {
	_, err := (&builder{reg: breakpointRegistry(new(bool)), pool: pool.New(0, 0)}).flow(types.FlowConfig{
		Process: []types.BlockConfig{
			{Type: blockKindSpy, Process: []types.BlockConfig{{Type: "mark"}}},
		},
	})
	if err == nil {
		t.Fatal("a hand-authored spy block must fail to build")
	}
	if !errors.Is(err, errSpyUnwired) {
		t.Errorf("error = %v, want errSpyUnwired", err)
	}
}
