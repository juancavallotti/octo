package builtin

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestMultiTransformAppliesStepsInOrder(t *testing.T) {
	// A later step reads the body a prior setBody produced and the variable a
	// prior setVar stored, proving the edits are additive within one block.
	proc, err := newMultiTransform(types.Settings{
		"transforms": []any{
			map[string]any{"setBody": `{"total": body.qty * body.price}`},
			map[string]any{"setVar": "total", "value": "body.total"},
			map[string]any{"setBody": `{"total": body.total, "doubled": vars.total * 2.0}`},
		},
	}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newMultiTransform: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = map[string]any{"qty": float64(3), "price": float64(10)}

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != msg {
		t.Fatal("multi-transform must forward the same message")
	}
	body, ok := msg.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want map", msg.Body)
	}
	if body["total"] != float64(30) {
		t.Errorf("total = %v, want 30", body["total"])
	}
	if body["doubled"] != float64(60) {
		t.Errorf("doubled = %v, want 60", body["doubled"])
	}
	if got, ok := msg.Variables.Int("total"); !ok || got != 30 {
		t.Errorf("vars.total = %d, %v; want 30, true", got, ok)
	}
}

func TestMultiTransformReadsEnv(t *testing.T) {
	proc, err := newMultiTransform(types.Settings{
		"transforms": []any{
			map[string]any{"setVar": "region", "value": "env.REGION"},
		},
	}, core.BlockDeps{Env: map[string]string{"REGION": "us-east"}})
	if err != nil {
		t.Fatalf("newMultiTransform: %v", err)
	}
	msg := mustMessage(t)
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got, ok := msg.Variables.String("region"); !ok || got != "us-east" {
		t.Errorf("region = %q, %v; want us-east, true", got, ok)
	}
}

func TestMultiTransformBuildValidation(t *testing.T) {
	tests := []struct {
		name string
		raw  types.Settings
	}{
		{name: "no transforms", raw: nil},
		{name: "empty transforms", raw: types.Settings{"transforms": []any{}}},
		{
			name: "step with neither setBody nor setVar",
			raw:  types.Settings{"transforms": []any{map[string]any{"value": "1"}}},
		},
		{
			name: "step with both setBody and setVar",
			raw:  types.Settings{"transforms": []any{map[string]any{"setBody": "body", "setVar": "x", "value": "1"}}},
		},
		{
			name: "setBody with a value",
			raw:  types.Settings{"transforms": []any{map[string]any{"setBody": "body", "value": "1"}}},
		},
		{
			name: "setVar without a value",
			raw:  types.Settings{"transforms": []any{map[string]any{"setVar": "x"}}},
		},
		{
			name: "bad expression",
			raw:  types.Settings{"transforms": []any{map[string]any{"setBody": "body."}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newMultiTransform(tt.raw, core.BlockDeps{}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
