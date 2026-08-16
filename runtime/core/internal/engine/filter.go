package engine

import (
	"context"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// httpStatusVar is the variable a block sets to choose the response status when
// the flow completes. It mirrors the http source's convention (see
// connectors/http/source.go); filter blocks write it on their built-in reject
// response so a rejected request completes with the right status.
const httpStatusVar = "httpStatus"

// applyReject configures the terminal response for a filter block that has
// decided to reject the message, then flags the message to stop the flow.
//
// When onReject is set it runs that sub-flow, which shapes the body/status
// itself; a sub-flow that drops the message (returns nil) drops it here too.
// Otherwise applyReject writes the given default status and body. In both cases
// the surviving message is marked via RequestStop, so the engine completes the
// flow with it instead of running the rest of the chain.
func applyReject(
	ctx context.Context,
	msg *types.Message,
	onReject *Flow,
	defaultStatus int,
	defaultBody any,
) (*types.Message, error) {
	if onReject != nil {
		out, err := onReject.Process(ctx, msg)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, nil
		}
		msg = out
	} else {
		msg.Variables.Set(httpStatusVar, defaultStatus)
		msg.SetBody(defaultBody)
	}
	msg.RequestStop()
	return msg, nil
}

// buildOnReject compiles a filter block's optional onReject sub-flow. It returns
// nil (use the built-in default response) when the slot is absent or carries no
// steps — the editor seeds an empty sub-flow into every composite slot, so an
// empty onReject must read as "unset" rather than an empty flow that suppresses
// the default.
func buildOnReject(b *builder, cfg *types.FlowConfig) (*Flow, error) {
	if cfg == nil || len(cfg.Process) == 0 {
		return nil, nil
	}
	flow, err := b.branch(core.BranchOnReject).subFlow(*cfg)
	if err != nil {
		return nil, fmt.Errorf("onReject: %w", err)
	}
	return flow, nil
}
