// Package engine assembles and runs message-processing flows. It is the runtime's
// pipeline implementation: the Flow type, the builder that turns a FlowConfig into
// a tree of leaf and composite blocks, the composite kinds (scope, fork, if,
// switch, foreach), and the built-in setter blocks. It is internal — callers wire
// flows through the public core package and the runtime service.
package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// Block type names handled directly by the flow builder rather than the block
// registry, because they compose sub-flows via typed config slots.
const (
	blockKindHandleErrors = "handle-errors"
	blockKindFork         = "fork"
	blockKindIf           = "if"
	blockKindSwitch       = "switch"
	blockKindForeach      = "foreach"
	blockKindAIRouter     = "ai-router"
	blockKindAIAgent      = "ai-agent"
	blockKindAIRetry      = "ai-retry"
)

// blockError wraps the error a block returns with the block's label. It keeps the
// label structured (rather than only in the formatted message) so recovery paths
// can recover the failing block via errors.As — see SetErrorVariable. Its Error
// text matches the previous fmt.Errorf("block %q: %w", ...) wrapping.
type blockError struct {
	label string
	err   error
}

func (e *blockError) Error() string { return fmt.Sprintf("block %q: %s", e.label, e.err) }

func (e *blockError) Unwrap() error { return e.err }

// Flow is an ordered sequence of blocks. It implements core.MessageProcessor by
// running a message through each block in order, and is the reusable unit that
// composite blocks embed. A Flow has no source; the runtime binds the root flow
// to a source. A nil result from a block drops the message (the chain stops); an
// error aborts it.
type Flow struct {
	Name   string
	Blocks []core.Block
}

// Process runs msg through the flow's blocks in order. It returns the final
// message, or (nil, nil) if a block dropped it, or an error if a block aborted.
func (f *Flow) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	current := msg
	for i := range f.Blocks {
		block := f.Blocks[i]
		out, err := block.Processor.Process(ctx, current)
		if err != nil {
			return nil, &blockError{label: blockLabel(block.Type, block.Name), err: err}
		}
		if out == nil {
			return nil, nil
		}
		current = out
	}
	return current, nil
}

// BuildRoot assembles the root Flow for a top-level flow config, resolving named
// processor definitions and threading the shared pool and block deps used by leaf
// and composite blocks.
func BuildRoot(
	cfg types.FlowConfig,
	blocks *core.BlockRegistry,
	p *pool.Pool,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
) (*Flow, error) {
	defs, err := processorDefs(processors)
	if err != nil {
		return nil, err
	}
	return (&builder{reg: blocks, pool: p, defs: defs, deps: deps}).flow(cfg)
}

// builder threads the shared context needed to assemble a flow tree: the leaf
// block registry, the shared worker pool composites schedule on, and the named
// processor definitions blocks resolve through ref.
type builder struct {
	reg  *core.BlockRegistry
	pool *pool.Pool
	defs map[string]types.ProcessorConfig
	deps core.BlockDeps
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
	blocks := make([]core.Block, 0, len(cfg.Process))
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
	if len(cfg.Error) > 0 {
		return nil, errors.New("sub-flow must not declare an error path")
	}
	return b.flow(cfg)
}

// block resolves a block's effective type and settings (applying ref), then
// builds its processor. Composite kinds build their typed sub-flows; any other
// type is a leaf resolved through the registry.
func (b *builder) block(cfg types.BlockConfig) (core.Block, error) {
	if cfg.Type == "" && cfg.Ref == "" {
		return core.Block{}, errors.New("block requires a type or a ref")
	}

	effType, effSettings, err := b.resolve(cfg)
	if err != nil {
		return core.Block{}, err
	}

	processor, err := b.processor(cfg, effType, effSettings)
	if err != nil {
		return core.Block{}, fmt.Errorf("block %q: %w", blockLabel(effType, cfg.Name), err)
	}
	return core.Block{Name: cfg.Name, Type: effType, Processor: processor}, nil
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
) (core.MessageProcessor, error) {
	switch effType {
	case blockKindHandleErrors:
		return b.handleErrors(cfg)
	case blockKindFork:
		return b.fork(cfg)
	case blockKindIf:
		return b.ifBlock(cfg)
	case blockKindSwitch:
		return b.switchBlock(cfg)
	case blockKindForeach:
		return b.foreachBlock(cfg)
	case blockKindAIRouter:
		return b.aiRouter(cfg)
	case blockKindAIAgent:
		return b.aiAgent(cfg)
	case blockKindAIRetry:
		return b.aiRetry(cfg)
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

// compositeSlots lists the composite-only fields a block config has set, by
// their YAML names. It is the single source of truth for which slots exist, so
// leaf and composite validation stay in sync as new composite kinds are added.
func compositeSlots(cfg types.BlockConfig) []string {
	var slots []string
	add := func(set bool, name string) {
		if set {
			slots = append(slots, name)
		}
	}
	add(len(cfg.Process) > 0, "process")
	add(len(cfg.Error) > 0, "error")
	add(len(cfg.Branches) > 0, "branches")
	add(cfg.Condition != "", "condition")
	add(cfg.Then != nil, "then")
	add(cfg.Else != nil, "else")
	add(len(cfg.Cases) > 0, "cases")
	add(cfg.Default != nil, "default")
	add(cfg.Items != "", "items")
	add(cfg.As != "", "as")
	add(cfg.Body != nil, "body")
	add(cfg.Connector != "", "connector")
	add(cfg.Prompt != "", "prompt")
	add(cfg.Guardrail != "", "guardrail")
	add(len(cfg.Routes) > 0, "routes")
	add(len(cfg.Tools) > 0, "tools")
	add(cfg.MaxIterations != 0, "maxIterations")
	add(cfg.MaxAttempts != 0, "maxAttempts")
	return slots
}

// rejectCompositeSlots fails if a leaf block carries any composite-only field.
func rejectCompositeSlots(cfg types.BlockConfig) error {
	if slots := compositeSlots(cfg); len(slots) > 0 {
		return fmt.Errorf("block %q is a leaf and must not declare composite slots %v", cfg.Type, slots)
	}
	return nil
}

// allowSlots restricts a composite to the given slots, rejecting any other
// composite slot it carries. The kind labels the error.
func allowSlots(cfg types.BlockConfig, kind string, allowed ...string) error {
	permitted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		permitted[name] = true
	}
	for _, slot := range compositeSlots(cfg) {
		if !permitted[slot] {
			return fmt.Errorf("%s block must not declare %q", kind, slot)
		}
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
