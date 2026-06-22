package core

import (
	"context"

	"github.com/juancavallotti/octo/types"
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
	Name      string
	Type      string
	Processor MessageProcessor
}

// BlockDeps carries build-time services a block factory may need beyond its
// settings. Most blocks ignore it. Connector resolves a configured connector
// instance by name so a block can use a capability that connector provides — for
// example, a log block binding to a logger connector. ok is false when no
// connector with that name is configured. Flows lets a block call another flow by
// name (used by the flow-ref block); it is nil when no flow caller is wired. Env
// holds the config's resolved environment variables so a block can expose them
// to its expressions as env.NAME; it is nil when none are declared.
type BlockDeps struct {
	Connector func(name string) (connector Connector, ok bool)
	Flows     FlowCaller
	Env       map[string]string
}

// BlockFactory builds a leaf processor from its settings and build-time deps.
// Composite kinds (scope, fork) are not built through the block registry; the
// flow builder recognizes them and constructs their typed sub-flows directly.
type BlockFactory func(settings types.Settings, deps BlockDeps) (MessageProcessor, error)
