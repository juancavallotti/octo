package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/juancavallotti/eip-go/types"
)

// Block type names handled directly by the flow builder rather than the block
// registry, because they compose sub-flows via typed config slots.
const (
	blockKindScope = "scope"
	blockKindFork  = "fork"
)

// Flow is an ordered sequence of blocks. It implements MessageProcessor by
// running a message through each block in order, and is the reusable unit that
// composite blocks embed. A Flow has no source; the runtime binds the root flow
// to a source. A nil result from a block drops the message (the chain stops); an
// error aborts it.
type Flow struct {
	Name   string
	Blocks []Block
}

// Process runs msg through the flow's blocks in order. It returns the final
// message, or (nil, nil) if a block dropped it, or an error if a block aborted.
func (f *Flow) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	current := msg
	for i := range f.Blocks {
		block := f.Blocks[i]
		out, err := block.Processor.Process(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("block %q: %w", blockLabel(block.Type, block.Name), err)
		}
		if out == nil {
			return nil, nil
		}
		current = out
	}
	return current, nil
}

// buildFlow assembles a Flow from a FlowConfig's block chain. It does not look at
// Source/Workers/Buffer; the caller decides whether those are allowed.
func buildFlow(cfg types.FlowConfig, reg *BlockRegistry) (*Flow, error) {
	blocks := make([]Block, 0, len(cfg.Process))
	for i := range cfg.Process {
		block, err := buildBlock(cfg.Process[i], reg)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return &Flow{Name: cfg.Name, Blocks: blocks}, nil
}

// buildSubFlow builds a nested flow, rejecting root-only fields.
func buildSubFlow(cfg types.FlowConfig, reg *BlockRegistry) (*Flow, error) {
	if cfg.Source != nil {
		return nil, errors.New("sub-flow must not declare a source")
	}
	if cfg.Workers != 0 || cfg.Buffer != 0 {
		return nil, errors.New("sub-flow must not declare workers or buffer")
	}
	return buildFlow(cfg, reg)
}

// buildBlock dispatches on block type: composite kinds build their typed
// sub-flows; any other type is a leaf resolved through the registry.
func buildBlock(cfg types.BlockConfig, reg *BlockRegistry) (Block, error) {
	if cfg.Type == "" {
		return Block{}, errors.New("block type is required")
	}

	processor, err := buildProcessor(cfg, reg)
	if err != nil {
		return Block{}, fmt.Errorf("block %q: %w", blockLabel(cfg.Type, cfg.Name), err)
	}
	return Block{Name: cfg.Name, Type: cfg.Type, Processor: processor}, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func buildProcessor(cfg types.BlockConfig, reg *BlockRegistry) (MessageProcessor, error) {
	switch cfg.Type {
	case blockKindScope:
		return buildScope(cfg, reg)
	case blockKindFork:
		return buildFork(cfg, reg)
	default:
		if err := rejectCompositeSlots(cfg); err != nil {
			return nil, err
		}
		return reg.New(cfg.Type, cfg.Settings)
	}
}

// rejectCompositeSlots fails if a leaf block carries composite-only fields.
func rejectCompositeSlots(cfg types.BlockConfig) error {
	if cfg.Main != nil || cfg.Alternative != nil || len(cfg.Branches) > 0 {
		return fmt.Errorf("block %q is a leaf and must not declare main/alternative/branches", cfg.Type)
	}
	return nil
}

// blockLabel returns a human-readable identifier for a block, preferring its
// name and falling back to its type.
func blockLabel(blockType, name string) string {
	if name != "" {
		return name
	}
	return blockType
}
