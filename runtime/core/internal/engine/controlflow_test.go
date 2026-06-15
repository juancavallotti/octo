package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/internal/pool"
	"github.com/juancavallotti/eip-go/types"
)

// recordRegistry extends the shared test registry with a "record" leaf that
// appends a marker to a shared slice, so tests can observe which sub-flows ran
// and in what order. A block records its "tag" setting verbatim, or the message
// variable named by its "var" setting (used to watch a foreach loop variable).
func recordRegistry(seen *[]any) *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("record", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		tag, hasTag := s.String("tag")
		varName, _ := s.String("var")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if hasTag {
				*seen = append(*seen, tag)
			} else {
				*seen = append(*seen, msg.Variables[varName])
			}
			return msg, nil
		}), nil
	})
	return reg
}

// tagFlow is a one-block flow that records the given marker when it runs.
func tagFlow(tag string) types.FlowConfig {
	return types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record", Settings: types.Settings{"tag": tag}},
	}}
}

//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func mustBuild(t *testing.T, reg *core.BlockRegistry, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	block, err := (&builder{reg: reg, pool: pool.New(0, 0)}).block(cfg)
	if err != nil {
		t.Fatalf("build %s: %v", cfg.Type, err)
	}
	return block.Processor
}

func TestIfRoutesOnCondition(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		withElse bool
		want     string
		passThru bool
	}{
		{name: "true runs then", amount: 200, withElse: true, want: "then"},
		{name: "false runs else", amount: 10, withElse: true, want: "else"},
		{name: "false no else passes through", amount: 10, withElse: false, passThru: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []any
			reg := recordRegistry(&seen)
			then := tagFlow("then")
			cfg := types.BlockConfig{Type: "if", Condition: "body.amount > 100", Then: &then}
			if tt.withElse {
				els := tagFlow("else")
				cfg.Else = &els
			}
			proc := mustBuild(t, reg, cfg)

			msg := mustMessage(t)
			msg.Body = map[string]any{"amount": tt.amount}
			out, err := proc.Process(context.Background(), msg)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if out != msg {
				t.Errorf("if returned %p, want input %p", out, msg)
			}
			if tt.passThru {
				if len(seen) != 0 {
					t.Errorf("expected no branch to run, saw %v", seen)
				}
				return
			}
			if len(seen) != 1 || seen[0] != tt.want {
				t.Errorf("seen = %v, want [%s]", seen, tt.want)
			}
		})
	}
}

func TestIfNonBoolConditionErrors(t *testing.T) {
	reg := testRegistry()
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:      "if",
		Condition: "body.amount",
		Then:      &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}},
	})

	msg := mustMessage(t)
	msg.Body = map[string]any{"amount": 5}
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Fatal("expected an error for a non-bool condition")
	}
}

func TestIfBuildValidation(t *testing.T) {
	reg := testRegistry()
	b := &builder{reg: reg, pool: pool.New(0, 0)}
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "missing condition", block: types.BlockConfig{Type: "if", Then: &types.FlowConfig{}}},
		{name: "missing then", block: types.BlockConfig{Type: "if", Condition: "true"}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type: "if", Condition: "true",
				Then:     &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}},
				Branches: []types.FlowConfig{{}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := b.block(tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestSwitchMatchesFirstCase(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		def      bool
		want     string
		passThru bool
	}{
		{name: "first case", kind: "a", def: true, want: "a"},
		{name: "second case", kind: "b", def: true, want: "b"},
		{name: "default", kind: "z", def: true, want: "default"},
		{name: "no match no default", kind: "z", def: false, passThru: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []any
			reg := recordRegistry(&seen)
			cfg := types.BlockConfig{Type: "switch", Cases: []types.CaseConfig{
				{When: `body.kind == "a"`, Flow: tagFlow("a")},
				{When: `body.kind == "b"`, Flow: tagFlow("b")},
			}}
			if tt.def {
				def := tagFlow("default")
				cfg.Default = &def
			}
			proc := mustBuild(t, reg, cfg)

			msg := mustMessage(t)
			msg.Body = map[string]any{"kind": tt.kind}
			if _, err := proc.Process(context.Background(), msg); err != nil {
				t.Fatalf("Process: %v", err)
			}
			if tt.passThru {
				if len(seen) != 0 {
					t.Errorf("expected no case to run, saw %v", seen)
				}
				return
			}
			if len(seen) != 1 || seen[0] != tt.want {
				t.Errorf("seen = %v, want [%s]", seen, tt.want)
			}
		})
	}
}

func TestSwitchBuildValidation(t *testing.T) {
	reg := testRegistry()
	b := &builder{reg: reg, pool: pool.New(0, 0)}
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "no cases", block: types.BlockConfig{Type: "switch"}},
		{name: "case without when", block: types.BlockConfig{Type: "switch", Cases: []types.CaseConfig{{Flow: tagFlow("x")}}}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type:  "switch",
				Cases: []types.CaseConfig{{When: "true", Flow: tagFlow("x")}},
				Main:  &types.FlowConfig{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := b.block(tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestForeachIteratesItems(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record", Settings: types.Settings{"var": "n"}},
	}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:  "foreach",
		Items: "body.nums",
		As:    "n",
		Body:  &body,
	})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != msg {
		t.Errorf("foreach returned %p, want input %p", out, msg)
	}
	if len(seen) != 3 || seen[0] != 1.0 || seen[2] != 3.0 {
		t.Errorf("seen = %v, want [1 2 3]", seen)
	}
	// The loop variable must not leak past the foreach.
	if _, ok := msg.Variables["n"]; ok {
		t.Error("loop variable n leaked past the foreach")
	}
}

func TestForeachRestoresPriorLoopVar(t *testing.T) {
	reg := testRegistry()
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type: "foreach", Items: "body.nums", As: "item", Body: &body,
	})

	msg := mustMessage(t)
	msg.Variables.Set("item", "original")
	msg.Body = map[string]any{"nums": []any{1.0, 2.0}}
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got, _ := msg.Variables.String("item"); got != "original" {
		t.Errorf("item = %q, want the restored \"original\"", got)
	}
}

func TestForeachNonArrayErrors(t *testing.T) {
	reg := testRegistry()
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type: "foreach", Items: "body.nums", Body: &body,
	})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": "not-an-array"}
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Fatal("expected an error iterating a non-array")
	}
}

func TestForeachBuildValidation(t *testing.T) {
	reg := testRegistry()
	b := &builder{reg: reg, pool: pool.New(0, 0)}
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "missing items", block: types.BlockConfig{Type: "foreach", Body: &types.FlowConfig{}}},
		{name: "missing body", block: types.BlockConfig{Type: "foreach", Items: "body.nums"}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type: "foreach", Items: "body.nums",
				Body: &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}},
				Then: &types.FlowConfig{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := b.block(tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
