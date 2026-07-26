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
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// Block type names handled directly by the flow builder rather than the block
// registry, because they compose sub-flows via typed config slots.
const (
	blockKindHandleErrors = "handle-errors"
	blockKindFork         = "fork"
	blockKindIf           = "if"
	blockKindSwitch       = "switch"
	blockKindForeach      = "foreach"
	blockKindEnrich       = "enrich"
	blockKindValidate     = "validate"
	blockKindCacheScope   = "cache-scope"
	blockKindAIRouter     = "ai-router"
	blockKindAIAgent      = "ai-agent"
	blockKindAIRetry      = "ai-retry"
	blockKindMCPRouter    = "mcp-router"
	// blockKindBreakpoint is injected by the runtime around an addressed block for
	// `invoke --break-at`; it is never authored in a flow (see breakpoint.go).
	blockKindBreakpoint = "breakpoint"
	// blockKindSpy is injected by the runtime around an addressed block for `invoke
	// --spies`; it is never authored in a flow either (see spy.go).
	blockKindSpy = "spy"
	// blockKindMock is injected by the runtime *in place of* an addressed block for
	// `invoke --mocks`; it is never authored in a flow either (see mock.go).
	blockKindMock = "mock"
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
	// flowName is the name of the root flow this one belongs to, which is what a
	// block event reports. It is not Name: a composite's sub-flow is usually
	// unnamed, and where it is named (a fork branch, a switch case) the name is the
	// branch's, not the flow's.
	flowName string
	// events dispatches this flow's block events. It is nil when nothing is
	// listening, which is the loop's fast exit.
	events *core.BlockEvents
}

// Process runs msg through the flow's blocks in order. It returns the final
// message, or (nil, nil) if a block dropped it, or an error if a block aborted.
//
// Each block is bracketed by a pre- and a post-invoke event when a dispatcher is
// wired and something is listening. Observing costs three predicted branches per
// block otherwise, and nothing is built, timed or allocated until a listener
// exists. Listeners observe: the outcome of the loop is the same either way.
func (f *Flow) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	current := msg
	for i := range f.Blocks {
		block := f.Blocks[i]

		observe := f.events != nil && block.Path != "" && f.events.Active()
		var started time.Time
		if observe {
			f.events.Emit(ctx, f.event(types.BlockPreInvoke, block, current, time.Now()))
			// Start the clock after the pre-invoke listeners, not before: a sync
			// listener runs inline, and a debugger parked at a breakpoint would
			// otherwise be billed to the block as a multi-second invocation.
			started = time.Now()
		}

		out, err := block.Processor.Process(ctx, current)

		if observe {
			f.events.Emit(ctx, f.outcome(block, current, out, err, started))
		}

		if err != nil {
			return nil, &blockError{label: blockLabel(block.Type, block.Name), err: err}
		}
		if out == nil {
			return nil, nil
		}
		current = out
		if current.StopRequested() {
			// A filter block requested stop: complete the flow now with the
			// message it configured, skipping the rest of the chain. Because
			// composite sub-flows return up through this same loop, the stop
			// bubbles to the root.
			return current, nil
		}
	}
	return current, nil
}

// event builds one block event for msg. Every field but the message is a copy
// taken here, on the flow's goroutine, so an async listener can read them safely.
func (f *Flow) event(
	kind types.BlockEventKind, block core.Block, msg *types.Message, at time.Time,
) types.BlockEvent {
	return types.BlockEvent{
		Kind:          kind,
		Flow:          f.flowName,
		Path:          block.Path,
		BlockType:     block.Type,
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		OccurredAt:    at,
		Message:       msg,
	}
}

// outcome builds the post-invoke event for all three outcomes. The message it
// carries is what the block produced, falling back to its input for the two
// outcomes that produce none — a drop and a failure — so a listener always has the
// message the block was working on.
func (f *Flow) outcome(
	block core.Block, in, out *types.Message, err error, started time.Time,
) types.BlockEvent {
	msg := out
	if msg == nil {
		msg = in
	}
	event := f.event(types.BlockPostInvoke, block, msg, time.Now())
	event.Duration = event.OccurredAt.Sub(started)
	event.Err = err
	event.Dropped = err == nil && out == nil
	return event
}

// BuildRoot assembles the root Flow for a top-level flow config, resolving named
// processor definitions and threading the shared pool and block deps used by leaf
// and composite blocks. Its blocks' paths root at the flow's own name.
func BuildRoot(
	cfg types.FlowConfig,
	blocks *core.BlockRegistry,
	p *pool.Pool,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
) (*Flow, error) {
	b, err := newBuilder(blocks, p, processors, deps, cfg.Name, cfg.Name)
	if err != nil {
		return nil, err
	}
	return b.flow(cfg)
}

// BuildErrorPath assembles the flow-level error chain as a root-level flow of its
// own: it owns the same pool and deps as the process chain, but its blocks' paths
// root at the flow's error chain (`<flow>[error]`) so they address the way the
// resolver reads them.
func BuildErrorPath(
	cfg types.FlowConfig,
	blocks *core.BlockRegistry,
	p *pool.Pool,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
) (*Flow, error) {
	b, err := newBuilder(blocks, p, processors, deps, cfg.Name, core.JoinBranch(cfg.Name, core.BranchError))
	if err != nil {
		return nil, err
	}
	return b.flow(types.FlowConfig{Name: cfg.Name, Process: cfg.Error})
}

// builder threads the shared context needed to assemble a flow tree: the leaf
// block registry, the shared worker pool composites schedule on, the named
// processor definitions blocks resolve through ref, and where in the flow the
// nodes it is building sit.
type builder struct {
	reg  *core.BlockRegistry
	pool *pool.Pool
	defs map[string]types.ProcessorConfig
	deps core.BlockDeps

	// flowName is the name of the root flow being built, constant for one build.
	// It is not Flow.Name, which is the (often empty) name of the sub-flow a
	// composite slot holds.
	flowName string

	// path is the address of the node whose children this builder builds:
	//
	//   - while building a chain, it is the slot the chain hangs off ("orders",
	//     "orders[error]", "orders.fanout[audit]"), and each block in the chain is
	//     path + "." + label;
	//   - while building one block's processor, it is that block's own address
	//     ("orders.fanout"), and each branch of it is path + "[" + branch + "]".
	//
	// The transition between the two happens exactly once, in block().
	path string
}

// newBuilder indexes the processor definitions and roots a builder at path.
func newBuilder(
	blocks *core.BlockRegistry,
	p *pool.Pool,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
	flowName, path string,
) (*builder, error) {
	defs, err := processorDefs(processors)
	if err != nil {
		return nil, err
	}
	return &builder{reg: blocks, pool: p, defs: defs, deps: deps, flowName: flowName, path: path}, nil
}

// at returns a builder that builds the children of the node at path.
func (b *builder) at(path string) *builder {
	child := *b
	child.path = path
	return &child
}

// branch returns the builder for one branch of the block this builder is on. It
// is what every composite builder descends through, so each sub-flow's blocks
// address as `<composite>[<branch>].<block>`.
func (b *builder) branch(name string) *builder {
	return b.at(core.JoinBranch(b.path, name))
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
	return &Flow{Name: cfg.Name, Blocks: blocks, flowName: b.flowName, events: b.deps.Events}, nil
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
//
// It is also where a block's address is minted, and where the builder crosses
// from "the chain at b.path" to "the block at b.path" — everything the processor
// builds below is a branch of this block.
func (b *builder) block(cfg types.BlockConfig) (core.Block, error) {
	if cfg.Type == "" && cfg.Ref == "" {
		return core.Block{}, errors.New("block requires a type or a ref")
	}

	effType, effSettings, err := b.resolve(cfg)
	if err != nil {
		return core.Block{}, err
	}

	// The address label reads the block as it was authored (name, else type, else
	// ref), not the effective type a ref resolves to — a block declared as
	// `{ref: charge-api}` is addressed by that ref. blockLabel below is the
	// separate, effective-type label an error carries.
	path := core.JoinBlock(b.path, core.BlockLabel(cfg.Name, cfg.Type, cfg.Ref))

	// A spy or breakpoint wrapper is transparent to an address: the injector gave
	// it its target's own label, so its inner chain hangs off the slot the wrapper
	// sits in rather than off a branch of it, and the wrapper itself reports no
	// path so the block it wraps is not observed twice. This is the mirror image of
	// unwrapDebug in core/runtime/address.go. A mock needs no such case: it stands
	// in its target's place rather than wrapping it, so its natural path is already
	// the address the user gave.
	inner, reported := path, path
	if isDebugWrapper(effType) {
		inner, reported = b.path, ""
	}

	processor, err := b.at(inner).processor(cfg, effType, effSettings)
	if err != nil {
		return core.Block{}, fmt.Errorf("block %q: %w", blockLabel(effType, cfg.Name), err)
	}
	return core.Block{Name: cfg.Name, Type: effType, Path: reported, Processor: processor}, nil
}

// isDebugWrapper reports whether the type is one of the wrappers the runtime
// injects around an addressed block, which stand in that block's place.
func isDebugWrapper(effType string) bool {
	return effType == blockKindSpy || effType == blockKindBreakpoint
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
	if build, ok := b.compositeBuilders()[effType]; ok {
		return build(cfg)
	}
	// Any non-composite type is a leaf resolved through the registry; it must not
	// carry composite slots.
	if err := rejectCompositeSlots(cfg); err != nil {
		return nil, err
	}
	return b.reg.New(effType, effSettings, b.deps)
}

// compositeBuilders maps each composite block kind to its builder. Composite
// kinds are handled directly by the flow builder (they compose sub-flows via
// typed config slots) rather than through the leaf block registry.
func (b *builder) compositeBuilders() map[string]func(types.BlockConfig) (core.MessageProcessor, error) {
	return map[string]func(types.BlockConfig) (core.MessageProcessor, error){
		blockKindHandleErrors: b.handleErrors,
		blockKindFork:         b.fork,
		blockKindIf:           b.ifBlock,
		blockKindSwitch:       b.switchBlock,
		blockKindForeach:      b.foreachBlock,
		blockKindEnrich:       b.enrich,
		blockKindValidate:     b.validateBlock,
		blockKindCacheScope:   b.cacheScope,
		blockKindAIRouter:     b.aiRouter,
		blockKindAIAgent:      b.aiAgent,
		blockKindAIRetry:      b.aiRetry,
		blockKindMCPRouter:    b.mcpRouter,
		blockKindBreakpoint:   b.breakpoint,
		blockKindSpy:          b.spy,
		blockKindMock:         b.mock,
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
	add(cfg.Mode != "", "mode")
	add(cfg.Body != nil, "body")
	add(cfg.SetBody != "", "setBody")
	add(len(cfg.SetVars) > 0, "setVars")
	add(cfg.Key != "", "key")
	add(cfg.TTL != "", "ttl")
	add(len(cfg.Rules) > 0, "rules")
	add(cfg.OnReject != nil, "onReject")
	add(cfg.RejectStatus != 0, "rejectStatus")
	add(cfg.Connector != "", "connector")
	add(cfg.Prompt != "", "prompt")
	add(cfg.Guardrail != "", "guardrail")
	add(len(cfg.Routes) > 0, "routes")
	add(len(cfg.Tools) > 0, "tools")
	add(len(cfg.Skills) > 0, "skills")
	add(cfg.MaxIterations != 0, "maxIterations")
	add(cfg.MaxAttempts != 0, "maxAttempts")
	add(cfg.ServerName != "", "serverName")
	add(len(cfg.Resources) > 0, "resources")
	add(len(cfg.Prompts) > 0, "prompts")
	add(cfg.MemoryThreadID != "", "memoryThreadId")
	add(cfg.MemoryMaxTokens != 0, "memoryMaxTokens")
	add(cfg.MemoryCompaction != "", "memoryCompaction")
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
