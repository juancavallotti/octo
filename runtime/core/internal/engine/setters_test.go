package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func TestSetPayloadReplacesBody(t *testing.T) {
	proc, err := newSetPayload(types.Settings{"value": `{"orders": [body.id]}`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newSetPayload: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = map[string]any{"id": "7"}

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != msg {
		t.Fatal("set-payload must forward the same message")
	}
	body, ok := msg.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want map", msg.Body)
	}
	orders, ok := body["orders"].([]any)
	if !ok || len(orders) != 1 || orders[0] != "7" {
		t.Errorf("body = %v, want {orders:[7]}", msg.Body)
	}
}

func TestSetPayloadReadsEnv(t *testing.T) {
	proc, err := newSetPayload(
		types.Settings{"value": `"region: " + env.REGION`},
		core.BlockDeps{Env: map[string]string{"REGION": "us-east"}},
	)
	if err != nil {
		t.Fatalf("newSetPayload: %v", err)
	}

	msg := mustMessage(t)
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if msg.Body != "region: us-east" {
		t.Errorf("body = %v, want %q", msg.Body, "region: us-east")
	}
}

func TestSetVariableStoresValue(t *testing.T) {
	proc, err := newSetVariable(types.Settings{"name": "threshold", "value": "100"}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newSetVariable: %v", err)
	}

	msg := mustMessage(t)
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got, ok := msg.Variables.Int("threshold"); !ok || got != 100 {
		t.Errorf("threshold = %d, %v; want 100, true", got, ok)
	}
}

func TestDeleteVariableRemovesValue(t *testing.T) {
	proc, err := newDeleteVariable(types.Settings{"name": "threshold"}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newDeleteVariable: %v", err)
	}

	msg := mustMessage(t)
	msg.Variables.Set("threshold", 100)
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if _, ok := msg.Variables.Int("threshold"); ok {
		t.Error("threshold should have been deleted")
	}
}

func TestDeleteVariableMissingIsNoOp(t *testing.T) {
	proc, err := newDeleteVariable(types.Settings{"name": "absent"}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newDeleteVariable: %v", err)
	}
	if _, err := proc.Process(context.Background(), mustMessage(t)); err != nil {
		t.Fatalf("Process on a missing variable: %v", err)
	}
}

func TestSetterBuildValidation(t *testing.T) {
	tests := []struct {
		name    string
		factory core.BlockFactory
		raw     types.Settings
	}{
		{name: "set-payload without value", factory: newSetPayload, raw: nil},
		{name: "set-payload bad expr", factory: newSetPayload, raw: types.Settings{"value": "body."}},
		{name: "set-variable without name", factory: newSetVariable, raw: types.Settings{"value": "1"}},
		{name: "set-variable without value", factory: newSetVariable, raw: types.Settings{"name": "x"}},
		{name: "set-variable bad expr", factory: newSetVariable, raw: types.Settings{"name": "x", "value": "body."}},
		{name: "delete-variable without name", factory: newDeleteVariable, raw: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.factory(tt.raw, core.BlockDeps{}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
