// Package engine is the runtime's flow runner. It holds the Flow type, the
// builder that turns a FlowConfig into a chain of blocks, and the machinery a
// block cannot supply for itself: block addresses, block events, continuations,
// and the sub-flow seam through which any block builds the nested chains it
// declares.
//
// The engine owns no block of its own. Every authored block type — leaf or
// composite — resolves through core.BlockRegistry, and a block that embeds
// sub-flows builds them through core.BlockDeps.SubFlows, which the builder
// implements. The first-party blocks live under runtime/blocks and register the
// same way a connector's blocks do. The only processors the engine constructs
// directly are the three the runtime injects for `invoke --break-at`, `--spies`
// and `--mocks`, which are never authored in a flow.
package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Block type names of the processors the runtime injects around (or in place
// of) an addressed block. They are handled by the builder rather than the
// registry because they are transparent to an address — see builder.block —
// and because nothing outside the runtime may declare them.
const (
	// blockKindBreakpoint is injected around an addressed block for `invoke
	// --break-at`; it is never authored in a flow (see breakpoint.go).
	blockKindBreakpoint = "breakpoint"
	// blockKindSpy is injected around an addressed block for `invoke --spies`;
	// it is never authored in a flow either (see spy.go).
	blockKindSpy = "spy"
	// blockKindMock is injected *in place of* an addressed block for `invoke
	// --mocks`; it is never authored in a flow either (see mock.go).
	blockKindMock = "mock"
)

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
	// resume schedules a resumed tail of this flow and reports its outcome. The
	// runtime installs it on a root flow (SetResume); it is nil for a sub-flow and
	// for a flow built outside the runtime, which is what makes a block that needs
	// a continuation fail loudly rather than silently drop the work it took over.
	//
	// It is read at resume time rather than captured, so it may be installed after
	// the flow — and its continuations — are built.
	resume core.FlowResume
}

// SetResume installs the hook a continuation uses to schedule the rest of this
// flow. Only the runtime calls it, on a root flow, before the source starts.
func (f *Flow) SetResume(r core.FlowResume) { f.resume = r }

// LifecycleProcessors returns this chain's blocks that own background work, in
// chain order. The runtime starts and stops them around the flow's own
// lifecycle. It looks only at this chain's blocks, not into composites' sub-flows.
func (f *Flow) LifecycleProcessors() []core.LifecycleProcessor {
	var procs []core.LifecycleProcessor
	for i := range f.Blocks {
		if proc, ok := f.Blocks[i].Processor.(core.LifecycleProcessor); ok {
			procs = append(procs, proc)
		}
	}
	return procs
}

// StateKeys returns the identities this chain's blocks store state under, so the
// runtime can reject a config where two of them would collide.
func (f *Flow) StateKeys() []string {
	var keys []string
	for i := range f.Blocks {
		if scoped, ok := f.Blocks[i].Processor.(core.StateScoped); ok {
			keys = append(keys, scoped.StateKey())
		}
	}
	return keys
}

// Process runs msg through the flow's blocks in order. It returns the final
// message, or (nil, nil) if a block dropped it, or an error if a block aborted.
//
// Each block is bracketed by a pre- and a post-invoke event when a dispatcher is
// wired and a listener asked for that block's path. A block nobody asked about
// costs two predicted branches and an atomic load, and nothing is built, timed or
// allocated for it — which is why the path is tested before the event is made
// rather than inside the listener. Listeners observe: the outcome of the loop is
// the same either way.
func (f *Flow) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	current := msg
	for i := range f.Blocks {
		block := f.Blocks[i]

		observe := f.events != nil && block.Path != "" && f.events.Observes(block.Path)
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
			return nil, &core.BlockError{Label: blockLabel(block.Type, block.Name), Err: err}
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
// processor definitions and threading the shared scheduler and block deps every
// block is built with. Its blocks' paths root at the flow's own name.
func BuildRoot(
	cfg types.FlowConfig,
	blocks *core.BlockRegistry,
	sched core.Scheduler,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
) (*Flow, error) {
	b, err := newBuilder(blocks, sched, processors, deps, cfg.Name, cfg.Name)
	if err != nil {
		return nil, err
	}
	return b.flow(cfg)
}

// BuildErrorPath assembles the flow-level error chain as a root-level flow of its
// own: it owns the same scheduler and deps as the process chain, but its blocks'
// paths root at the flow's error chain (`<flow>[error]`) so they address the way
// the resolver reads them.
func BuildErrorPath(
	cfg types.FlowConfig,
	blocks *core.BlockRegistry,
	sched core.Scheduler,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
) (*Flow, error) {
	b, err := newBuilder(blocks, sched, processors, deps, cfg.Name, core.JoinBranch(cfg.Name, core.BranchError))
	if err != nil {
		return nil, err
	}
	return b.flow(types.FlowConfig{Name: cfg.Name, Process: cfg.Error})
}

// builder threads the shared context needed to assemble a flow tree: the block
// registry, the scheduler blocks run concurrent work on, the named processor
// definitions blocks resolve through ref, and where in the flow the nodes it is
// building sit.
type builder struct {
	reg   *core.BlockRegistry
	sched core.Scheduler
	defs  map[string]types.ProcessorConfig
	deps  core.BlockDeps

	// flowName is the name of the root flow being built, constant for one build.
	// It is not Flow.Name, which is the (often empty) name of the sub-flow a
	// block's slot holds.
	flowName string

	// root reports that this builder is assembling a root chain — a flow's own
	// process or error chain — rather than a block's sub-flow. Only a root chain
	// has a meaningful "rest of the flow" to hand a block that takes over, so it
	// is both where continuations are wired and what SubFlowBuilder.Root reports.
	root bool

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
	sched core.Scheduler,
	processors []types.ProcessorConfig,
	deps core.BlockDeps,
	flowName, path string,
) (*builder, error) {
	defs, err := processorDefs(processors)
	if err != nil {
		return nil, err
	}
	return &builder{
		reg: blocks, sched: sched, defs: defs, deps: deps,
		flowName: flowName, path: path, root: true,
	}, nil
}

// at returns a builder that builds the children of the node at path.
func (b *builder) at(path string) *builder {
	child := *b
	child.path = path
	return &child
}

// branch returns the builder for one branch of the block this builder is on. It
// is what every sub-flow descends through, so each sub-flow's blocks address as
// `<block>[<branch>].<step>`.
func (b *builder) branch(name string) *builder {
	child := b.at(core.JoinBranch(b.path, name))
	// Descending into a branch leaves the root chain: the sub-flow ends at the
	// block that owns it, which then post-processes the result, so there is no
	// "rest of the flow" to continue into from in here.
	child.root = false
	return child
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
	flow := &Flow{Name: cfg.Name, Blocks: blocks, flowName: b.flowName, events: b.deps.Events}
	if b.root {
		if err := flow.wireContinuations(); err != nil {
			return nil, err
		}
	}
	return flow, nil
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
// builds its processor through the registry.
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

	processor, err := b.at(inner).processor(cfg.Name, effType, effSettings)
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

// processor builds the block at b.path. The injected debug processors are built
// here, because only the builder knows the transparency rule their addresses
// follow; every other type is resolved through the registry, with the deps
// positioned at this block so a factory can build the sub-flows it declares.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) processor(name, effType string, effSettings types.Settings) (core.MessageProcessor, error) {
	// b is the copy at() returned in block(), positioned at this block, so b.path is
	// this block's own address and writing to b.deps here cannot leak sideways into
	// a sibling.
	b.deps.Address = core.BlockAddress{Flow: b.flowName, Path: b.path, Name: name}
	b.deps.SubFlows = subFlows{b: b}
	b.deps.Scheduler = b.sched

	switch effType {
	case blockKindBreakpoint:
		return b.breakpoint(effSettings)
	case blockKindSpy:
		return b.spy(effSettings)
	case blockKindMock:
		return b.mock(effSettings)
	default:
		return b.reg.New(effType, effSettings, b.deps)
	}
}

// subFlows is the engine's core.SubFlowBuilder: a builder positioned at the
// block being built, handed to that block's factory through BlockDeps.
type subFlows struct {
	b *builder
}

// Branch builds the chain under a named slot of the block being built.
//
//nolint:ireturn // the seam returns the MessageProcessor interface by design
func (s subFlows) Branch(name string, flow types.FlowConfig) (core.MessageProcessor, error) {
	return s.b.branch(name).subFlow(flow)
}

// Member builds one entry of a list-valued slot, addressed by its own name or
// its index.
//
//nolint:ireturn // the seam returns the MessageProcessor interface by design
func (s subFlows) Member(name string, index int, flow types.FlowConfig) (core.MessageProcessor, error) {
	return s.b.branch(core.MemberBranch(name, index)).subFlow(flow)
}

// Root reports whether the block being built sits in a root chain.
func (s subFlows) Root() bool { return s.b.root }

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

// blockLabel returns a human-readable identifier for a block, preferring its
// name and falling back to its type.
func blockLabel(blockType, name string) string {
	if name != "" {
		return name
	}
	return blockType
}
