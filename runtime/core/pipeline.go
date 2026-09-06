package core

import (
	"context"

	"github.com/juancavallotti/octo/runtime/types"
)

// MessageProcessor transforms a single message. It is the unit of work a Block
// wraps. Because one processor instance is shared across all workers in a flow,
// implementations must be safe for concurrent use.
//
// Returning (nil, nil) drops the message: it is filtered out and the rest of the
// chain is skipped. A non-nil error aborts the message.
type MessageProcessor interface {
	Process(ctx context.Context, msg *types.Message) (*types.Message, error)
}

// ContinuationAware is an optional capability a processor implements when it
// takes over the flow rather than returning the next message. The flow builder
// hands it the Continuation covering the blocks that follow it, once the flow is
// assembled.
//
// It is wired only for a block in a root chain. A composite's sub-flow ends at
// the composite, which then post-processes the result, so there is no meaningful
// "rest of the flow" to hand down; a processor that needs a continuation and is
// given none must fail its build rather than run without one.
type ContinuationAware interface {
	SetContinuation(c Continuation)
}

// LifecycleProcessor is an optional capability a processor implements when it
// owns background work — a ticker, a leadership lease, anything that outlives a
// single Process call. The runtime starts it after the flow's pool is up and
// before the source admits traffic, and stops it after in-flight messages drain
// and before the pool is torn down, so nothing it started can submit to a pool
// that is already stopping.
type LifecycleProcessor interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// StateScoped is an optional capability a processor implements when it owns
// persistent state under a key that must be its alone. Two blocks sharing one
// would silently read and write each other's state, so the runtime collects the
// keys across every flow and refuses a config that repeats one.
type StateScoped interface {
	StateKey() string
}

// Block is a configured, named stage in a flow wrapping one MessageProcessor.
// The block itself stays a thin record; any sub-flows a block embeds live inside
// its processor.
type Block struct {
	Name string
	Type string
	// Path is the block's address in the runtime's grammar (see blockpath.go),
	// minted by the flow builder from the block's position in the config. It is
	// what a block event reports as the place it came from.
	//
	// It is empty for a block that must not report: the implicit spy and
	// breakpoint wrappers, which stand in their target's place and would
	// otherwise emit a second event at the same address.
	//
	// The path is an observability label, not a resolvable handle. Two cases mint
	// one the resolver would not accept back: two unnamed blocks of the same type
	// in one chain share a path, and a name carrying a '.', '[' or ']' — which
	// nothing rejects today — produces segments the parser splits the wrong way.
	// Minting nothing in those cases would be worse than minting a label that is
	// shared or unparseable, since it would leave those blocks unobservable.
	Path      string
	Processor MessageProcessor
}

// BlockAddress is where the block being built sits: the root flow it belongs to,
// its address within that flow, and the name it was authored with.
//
// A block cannot derive any of this for itself. A composite is built by the flow
// builder and could read the builder's own position, but a leaf is built through
// the registry from nothing but its settings — so a block that has to report on
// itself, such as an AI block recording what a model turn cost, has no way to say
// where the cost was incurred. This is that way.
//
// The same "observability label, not a resolvable handle" caveat Block.Path
// carries applies here, for the same reasons: two unnamed blocks of the same type
// in one chain share an address, and a name carrying '.', '[' or ']' mints
// segments the address parser splits the wrong way.
type BlockAddress struct {
	// Flow is the root flow's name. Path already begins with it; it is carried
	// separately so a record can be filtered by flow without parsing an address.
	Flow string
	// Path is the block's address in the grammar blockpath.go defines. It is the
	// same string the block's own Block.Path carries, minted once by the builder.
	Path string
	// Name is the name the block was authored with, empty for an unnamed block.
	// Path falls back to the type for those, so the two are not interchangeable.
	Name string
}

// BlockDeps carries build-time services a block factory may need beyond its
// settings. Most blocks ignore it. Connector resolves a configured connector
// instance by name so a block can use a capability that connector provides — for
// example, a log block binding to a logger connector. ok is false when no
// connector with that name is configured. Flows lets a block call another flow by
// name (used by the flow-ref block); it is nil when no flow caller is wired. Env
// holds the config's resolved environment variables so a block can expose them
// to its expressions as env.NAME; it is nil when none are declared. Services
// exposes the runtime services (leader election, KV) to a block; it is nil for
// callers that do not wire them, so a block must guard against that.
type BlockDeps struct {
	Connector func(name string) (connector Connector, ok bool)
	Flows     FlowCaller
	Env       map[string]string
	Services  RuntimeServices
	// Resources loads resources (templates, env files) a block may need, e.g. the
	// template-resource block reading a template by id. It is nil when no loader is
	// wired; a block must guard against that (or the caller supplies a Noop).
	Resources ResourceLoader
	// Breakpoint collects the message at an addressed block and halts the flow, for
	// the CLI's `invoke --break-at`. It is nil in every normal run — only the
	// implicit breakpoint block reads it, and it refuses to build without one, so a
	// flow can never carry a breakpoint that was not asked for.
	Breakpoint *Breakpoint
	// Spies collects what crosses each addressed block, for the CLI's `invoke
	// --spies`. Nil in every normal run, on the same terms as Breakpoint: only the
	// implicit spy block reads it, and it refuses to build without one.
	Spies *Spies
	// Mocks holds the canned outcomes each addressed block is replaced with, for the
	// CLI's `invoke --mocks`. Unlike the two above it collects nothing — it is an
	// input to the run — but it reaches the engine the same way, and the implicit
	// mock block refuses to build without it, so a flow can never carry a mock that
	// was not asked for.
	Mocks *Mocks
	// Events dispatches the pre- and post-invoke events emitted around every block.
	// Unlike the rest of BlockDeps it is read by the flow itself rather than by a
	// block factory — it arrives here because this is already the channel through
	// which the runtime hands build-time services to the engine.
	//
	// It is nil when no dispatcher is wired, which is the engine's fast exit: a nil
	// check per block is the whole cost of the feature for a flow nobody observes.
	Events *BlockEvents
	// Address is where the block being built sits in the flow being built. Unlike
	// the rest of BlockDeps it is not a service — it is the one piece of build-time
	// context a block cannot derive for itself, and it changes per block rather
	// than per runtime. The flow builder fills it in for every block.
	Address BlockAddress
	// SubFlows builds the nested chains a block declares, positioned at the block
	// being built. It is what lets any block own a sub-flow slot: the block reads
	// the slot out of its settings and hands it here. Nil when the block is built
	// outside the engine, which a block with a slot must treat as a build error
	// (see SubFlowsOf).
	SubFlows SubFlowBuilder
	// Scheduler is the flow's shared worker pool, for a block that runs work
	// concurrently. Nil outside the engine, on the same terms as SubFlows.
	Scheduler Scheduler
}

// BlockFactory builds a processor from its settings and build-time deps. Every
// block is built this way, whether it is a leaf or one that embeds sub-flows: a
// composite reads its slots out of the settings and builds them through
// deps.SubFlows.
type BlockFactory func(settings types.Settings, deps BlockDeps) (MessageProcessor, error)
