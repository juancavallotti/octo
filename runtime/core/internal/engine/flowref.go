// Built-in flow-ref block: invokes another flow by name. It registers on the
// process-wide block registry like the other built-ins. A flow-ref calls a flow
// that has no external source (an implicit-source flow), reached through the flow
// caller wired into BlockDeps.
package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("flow-ref", newFlowRef)
}

// flowRefSettings configures the flow-ref block.
type flowRefSettings struct {
	// Flow is the name of the flow to invoke.
	Flow string `json:"flow"`
	// OneWay fires the call and returns immediately, ignoring the result. When
	// false (the default) the block waits for the called flow and folds its
	// result back into the current message.
	OneWay bool `json:"oneWay"`
}

// flowRef invokes another flow by name. Two-way (the default) waits for the
// result and merges it back; one-way fires and forgets.
type flowRef struct {
	caller core.FlowCaller
	flow   string
	oneWay bool
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newFlowRef(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg flowRefSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Flow == "" {
		return nil, errors.New("flow-ref requires a flow name")
	}
	if deps.Flows == nil {
		return nil, errors.New("flow-ref requires a flow caller (not available in this context)")
	}
	return &flowRef{caller: deps.Flows, flow: cfg.Flow, oneWay: cfg.OneWay}, nil
}

// Process invokes the target flow. A fresh sub-message (new EventID, cloned body
// and variables) is sent so the sub-invocation correlates independently of this
// flow's own terminal event. One-way returns the message unchanged; two-way folds
// the called flow's body and variables back into the message.
func (f *flowRef) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	sub := msg.Clone()
	if _, err := sub.Rekey(); err != nil {
		return nil, fmt.Errorf("flow-ref %q: %w", f.flow, err)
	}

	if f.oneWay {
		if err := f.caller.Send(ctx, f.flow, sub); err != nil {
			return nil, fmt.Errorf("flow-ref %q: %w", f.flow, err)
		}
		return msg, nil
	}

	result, err := f.caller.Call(ctx, f.flow, sub)
	if err != nil {
		return nil, fmt.Errorf("flow-ref %q: %w", f.flow, err)
	}
	if result == nil {
		// The called flow dropped the message; leave the current message as-is.
		return msg, nil
	}

	msg.Body = result.Body
	for k, v := range result.Variables {
		msg.Variables.Set(k, v)
	}
	return msg, nil
}
