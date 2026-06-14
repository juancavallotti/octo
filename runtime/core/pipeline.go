package core

import (
	"context"

	"github.com/juancavallotti/eip-go/types"
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

// BlockFactory builds a leaf processor from its settings. Composite kinds (scope,
// fork) are not built through the block registry; the flow builder recognizes
// them and constructs their typed sub-flows directly.
type BlockFactory func(settings map[string]any) (MessageProcessor, error)
