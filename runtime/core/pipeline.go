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

// Block is a configured, named stage in a flow wrapping one MessageProcessor.
// The Processor is either a leaf (built from a BlockFactory) or a composite that
// embeds sub-flows (built by the flow builder). The block itself stays a thin
// record; any embedded flows live inside the composite processor.
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
	// The path is an observability label, not a resolvable handle. Two unnamed
	// blocks of the same type in one chain mint the same path, where the address
	// resolver would refuse the ambiguity — minting nothing for them would be
	// worse than minting a label that happens to be shared.
	Path      string
	Processor MessageProcessor
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
}

// BlockFactory builds a leaf processor from its settings and build-time deps.
// Composite kinds (scope, fork) are not built through the block registry; the
// flow builder recognizes them and constructs their typed sub-flows directly.
type BlockFactory func(settings types.Settings, deps BlockDeps) (MessageProcessor, error)
