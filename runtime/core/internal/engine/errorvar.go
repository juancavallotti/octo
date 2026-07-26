package engine

import (
	"errors"

	"github.com/juancavallotti/octo/runtime/types"
)

// errorVarName is the message variable under which a processing error is exposed
// to recovery paths (the flow-level error chain and the handle-errors block).
const errorVarName = "error"

// FailingBlock returns the label of the block a processing error originated in,
// or "" when the error did not come from a block. The engine wraps every block
// failure so the label stays structured rather than only formatted into the
// message (see blockError), and this is how callers outside the engine read it
// back: the label is the only part that escapes, so blockError stays unexported.
//
// It unwraps, so an error the runtime has re-wrapped — a failing error path is
// reported as `error path: %w` — still names the block inside it.
func FailingBlock(err error) string {
	var be *blockError
	if errors.As(err, &be) {
		return be.label
	}
	return ""
}

// SetErrorVariable exposes a processing error to a recovery path as the structured
// message variable vars.error:
//
//	{ "message": <err.Error()>, "flow": <name>, "block": <failing block label> }
//
// name is the enclosing flow or handle-errors block name. block is recovered from
// the error chain when the error originated in a leaf block, and is empty
// otherwise. It is a map[string]any so CEL expressions can read
// vars.error.message, vars.error.flow, and vars.error.block.
func SetErrorVariable(msg *types.Message, name string, err error) {
	msg.Variables.Set(errorVarName, map[string]any{
		"message": err.Error(),
		"flow":    name,
		"block":   FailingBlock(err),
	})
}
