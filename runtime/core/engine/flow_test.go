package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestFlowProcessOutcomes(t *testing.T) {
	reg := testRegistry()

	tests := []struct {
		name      string
		blocks    []types.BlockConfig
		wantNil   bool
		wantError bool
	}{
		{name: "pass-through", blocks: []types.BlockConfig{{Type: "pass"}}},
		{name: "drop", blocks: []types.BlockConfig{{Type: "drop"}, {Type: "pass"}}, wantNil: true},
		{name: "abort", blocks: []types.BlockConfig{{Type: "fail"}}, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow, err := (&builder{reg: reg, sched: pool.New(0, 0)}).flow(types.FlowConfig{Process: tt.blocks})
			if err != nil {
				t.Fatalf("buildFlow: %v", err)
			}
			out, err := flow.Process(context.Background(), mustMessage(t))
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if tt.wantNil != (out == nil) {
				t.Errorf("out == nil is %v, want %v", out == nil, tt.wantNil)
			}
		})
	}
}

func TestBuildBlockValidation(t *testing.T) {
	reg := testRegistry()

	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "empty type", block: types.BlockConfig{}},
		{name: "unregistered leaf", block: types.BlockConfig{Type: "nope"}},
		{name: "handle-errors without chains", block: types.BlockConfig{Type: "handle-errors"}},
		{name: "fork without branches", block: types.BlockConfig{Type: "fork"}},
		{
			name: "sub-flow with source",
			block: types.BlockConfig{
				Type:     "fork",
				Settings: types.Settings{"branches": []types.FlowConfig{{Source: &types.SourceConfig{Connector: "x"}}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&builder{reg: reg, sched: pool.New(0, 0)}).block(tt.block); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestBuildCompositeDispatch(t *testing.T) {
	reg := testRegistry()

	cfg := types.BlockConfig{
		Type:     "handle-errors",
		Settings: types.Settings{"process": []types.BlockConfig{{Type: "fail"}}, "error": []types.BlockConfig{{Type: "pass"}}}}
	block, err := (&builder{reg: reg, sched: pool.New(0, 0)}).block(cfg)
	if err != nil {
		t.Fatalf("buildBlock: %v", err)
	}

	// process fails, so handle-errors must fall back to the error chain and recover.
	out, err := block.Processor.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("handle-errors Process: %v", err)
	}
	if out == nil {
		t.Fatal("expected recovered message, got nil")
	}
}
