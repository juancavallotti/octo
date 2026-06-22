package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// captureRegistry registers a leaf block that records the settings it was built
// with, so tests can assert how a ref resolves to effective settings.
func captureRegistry(into *types.Settings) *core.BlockRegistry {
	reg := core.NewBlockRegistry()
	reg.MustRegister("capture", func(settings types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		*into = settings
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			return msg, nil
		}), nil
	})
	return reg
}

func TestBlockRefMergesSettings(t *testing.T) {
	var got types.Settings
	defs, err := processorDefs([]types.ProcessorConfig{
		{Name: "base", Type: "capture", Settings: types.Settings{"a": 1, "b": 2}},
	})
	if err != nil {
		t.Fatalf("processorDefs: %v", err)
	}

	b := &builder{reg: captureRegistry(&got), pool: pool.New(0, 0), defs: defs}
	block, err := b.block(types.BlockConfig{Ref: "base", Settings: map[string]any{"b": 99, "c": 3}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	if block.Type != "capture" {
		t.Errorf("effective type = %q, want capture", block.Type)
	}
	// Block-level settings override the referenced ones key-by-key.
	if got["a"] != 1 || got["b"] != 99 || got["c"] != 3 {
		t.Errorf("effective settings = %v, want {a:1 b:99 c:3}", got)
	}
}

func TestBlockRefErrors(t *testing.T) {
	defs := map[string]types.ProcessorConfig{"base": {Name: "base", Type: "pass"}}
	b := &builder{reg: testRegistry(), pool: pool.New(0, 0), defs: defs}

	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "unknown ref", block: types.BlockConfig{Ref: "missing"}},
		{name: "type mismatch", block: types.BlockConfig{Ref: "base", Type: "drop"}},
		{name: "neither type nor ref", block: types.BlockConfig{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := b.block(tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestProcessorDefsRejectsDuplicates(t *testing.T) {
	if _, err := processorDefs([]types.ProcessorConfig{
		{Name: "x", Type: "pass"},
		{Name: "x", Type: "drop"},
	}); err == nil {
		t.Fatal("expected a duplicate-name error")
	}
}
