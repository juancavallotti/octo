package core

import (
	"context"

	"github.com/juancavallotti/eip-go/types"
)

// FlowCaller invokes a registered flow by name. It is the contract behind direct
// invocation (the CLI) and the flow-ref block: a flow without an external source
// registers its input channel under its name, and callers push messages into it,
// correlating the result through the flow-event bus.
//
// Outcomes follow the flow's terminal event: a completed flow returns its result
// message, a dropped flow returns (nil, nil), and a failed flow returns the error.
type FlowCaller interface {
	// Call sends msg to the named flow and waits for its terminal outcome.
	// completed -> (result, nil); dropped -> (nil, nil); failed -> (nil, err).
	Call(ctx context.Context, name string, msg *types.Message) (*types.Message, error)
	// Send delivers msg to the named flow without awaiting a result (one-way).
	Send(ctx context.Context, name string, msg *types.Message) error
}
