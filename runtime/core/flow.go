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

// builder threads the shared context needed to assemble a flow tree: the leaf
// block registry, the shared worker pool composites schedule on, and the named
// processor definitions blocks resolve through ref.
type builder struct {
	reg  *BlockRegistry
	pool *pool
	defs map[string]types.ProcessorConfig
	deps BlockDeps
}

// processorDefs indexes named processor definitions by name, rejecting
// duplicates so a ref resolves unambiguously.
func processorDefs(configs []types.ProcessorConfig) (map[string]types.ProcessorConfig, error) {
	defs := make(map[string]types.ProcessorConfig, len(configs))
	for _, cfg := range configs {
		if cfg.Name == "" {
			return nil, errors.New("processor definition requires a name")
		}
		if _, dup := defs[cfg.Name]; dup {
			return nil, fmt.Errorf("processor %q is defined more than once", cfg.Name)
		}
		defs[cfg.Name] = cfg
	}
	return defs, nil
}

// flow assembles a Flow from a FlowConfig's block chain. It does not look at
// Source/Workers/Buffer/Pool; the caller decides whether those are allowed.
func (b *builder) flow(cfg types.FlowConfig) (*Flow, error) {
	blocks := make([]Block, 0, len(cfg.Process))
	for i := range cfg.Process {
		block, err := b.block(cfg.Process[i])
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return &Flow{Name: cfg.Name, Blocks: blocks}, nil
}

// subFlow builds a nested flow, rejecting root-only fields.
func (b *builder) subFlow(cfg types.FlowConfig) (*Flow, error) {
	if cfg.Source != nil {
		return nil, errors.New("sub-flow must not declare a source")
	}
	if cfg.Workers != 0 || cfg.Buffer != 0 || cfg.Pool != 0 {
		return nil, errors.New("sub-flow must not declare workers, buffer, or pool")
	}
	return b.flow(cfg)
}

// block resolves a block's effective type and settings (applying ref), then
// builds its processor. Composite kinds build their typed sub-flows; any other
// type is a leaf resolved through the registry.
func (b *builder) block(cfg types.BlockConfig) (Block, error) {
	if cfg.Type == "" && cfg.Ref == "" {
		return Block{}, errors.New("block requires a type or a ref")
	}

	effType, effSettings, err := b.resolve(cfg)
	if err != nil {
		return Block{}, err
	}

	processor, err := b.processor(cfg, effType, effSettings)
	if err != nil {
		return Block{}, fmt.Errorf("block %q: %w", blockLabel(effType, cfg.Name), err)
	}
	return Block{Name: cfg.Name, Type: effType, Processor: processor}, nil
}

// resolve applies a block's ref, returning the effective type and settings. When
// ref is empty the block is inline. When set, the named definition supplies the
// type and base settings; the block's own settings override them key-by-key.
func (b *builder) resolve(cfg types.BlockConfig) (string, types.Settings, error) {
	if cfg.Ref == "" {
		return cfg.Type, cfg.Settings, nil
	}
	def, ok := b.defs[cfg.Ref]
	if !ok {
		return "", nil, fmt.Errorf("block ref %q is not a defined processor", cfg.Ref)
	}
	if cfg.Type != "" && cfg.Type != def.Type {
		return "", nil, fmt.Errorf("block ref %q is type %q but declares type %q", cfg.Ref, def.Type, cfg.Type)
	}
	return def.Type, mergeSettings(def.Settings, cfg.Settings), nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) processor(
	cfg types.BlockConfig, effType string, effSettings types.Settings,
) (MessageProcessor, error) {
	switch effType {
	case blockKindScope:
		return b.scope(cfg)
	case blockKindFork:
		return b.fork(cfg)
	default:
		if err := rejectCompositeSlots(cfg); err != nil {
			return nil, err
		}
		return b.reg.New(effType, effSettings, b.deps)
	}
}

// mergeSettings overlays override onto base without mutating either, with
// override keys winning. It returns base unchanged when there is nothing to
// overlay.
func mergeSettings(base, override types.Settings) types.Settings {
	if len(override) == 0 {
		return base
	}
	merged := make(types.Settings, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
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
