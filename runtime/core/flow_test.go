package core

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

// processorFunc adapts a function to the MessageProcessor interface for tests.
type processorFunc func(ctx context.Context, msg *types.Message) (*types.Message, error)

func (f processorFunc) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	return f(ctx, msg)
}

// testRegistry returns a registry with leaf blocks used across flow tests.
func testRegistry() *BlockRegistry {
	reg := NewBlockRegistry()
	reg.MustRegister("pass", func(types.Settings, BlockDeps) (MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			return msg, nil
		}), nil
	})
	reg.MustRegister("drop", func(types.Settings, BlockDeps) (MessageProcessor, error) {
		return processorFunc(func(context.Context, *types.Message) (*types.Message, error) {
			return nil, nil
		}), nil
	})
	reg.MustRegister("fail", func(types.Settings, BlockDeps) (MessageProcessor, error) {
		return processorFunc(func(context.Context, *types.Message) (*types.Message, error) {
			return nil, errors.New("boom")
		}), nil
	})
	return reg
}

func mustMessage(t *testing.T) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	return msg
}

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
			flow, err := (&builder{reg: reg, pool: newPool(0, 0)}).flow(types.FlowConfig{Process: tt.blocks})
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
		{name: "leaf with slots", block: types.BlockConfig{Type: "pass", Branches: []types.FlowConfig{{}}}},
		{name: "scope without main", block: types.BlockConfig{Type: "scope"}},
		{name: "fork without branches", block: types.BlockConfig{Type: "fork"}},
		{
			name: "sub-flow with source",
			block: types.BlockConfig{
				Type: "scope",
				Main: &types.FlowConfig{Source: &types.SourceConfig{Connector: "x"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&builder{reg: reg, pool: newPool(0, 0)}).block(tt.block); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestBuildCompositeDispatch(t *testing.T) {
	reg := testRegistry()

	cfg := types.BlockConfig{
		Type: "scope",
		Main: &types.FlowConfig{Process: []types.BlockConfig{{Type: "fail"}}},
		Alternative: &types.FlowConfig{
			Process: []types.BlockConfig{{Type: "pass"}},
		},
	}
	block, err := (&builder{reg: reg, pool: newPool(0, 0)}).block(cfg)
	if err != nil {
		t.Fatalf("buildBlock: %v", err)
	}

	// main fails, so the scope must fall back to the alternative and recover.
	out, err := block.Processor.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("scope Process: %v", err)
	}
	if out == nil {
		t.Fatal("expected recovered message, got nil")
	}
}
