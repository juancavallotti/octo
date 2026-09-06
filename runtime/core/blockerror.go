package core

import (
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/runtime/types"
)

// ErrorVariable is the message variable under which a processing error is
// exposed to recovery paths: the flow-level error chain, and any block that runs
// an alternative chain when its protected one fails (handle-errors, ai-retry).
const ErrorVariable = "error"

// BlockError wraps the error a block returns with the block's label. The engine
// wraps every block failure this way so the label stays structured, rather than
// only formatted into the message, and a recovery path can recover the failing
// block through errors.As — see FailingBlock. Its text reads `block "x": …`.
type BlockError struct {
	Label string
	Err   error
}

func (e *BlockError) Error() string { return fmt.Sprintf("block %q: %s", e.Label, e.Err) }

func (e *BlockError) Unwrap() error { return e.Err }

// FailingBlock returns the label of the block a processing error originated in,
// or "" when the error did not come from a block.
//
// It unwraps, so an error the runtime has re-wrapped — a failing error path is
// reported as `error path: %w` — still names the block inside it.
func FailingBlock(err error) string {
	var be *BlockError
	if errors.As(err, &be) {
		return be.Label
	}
	return ""
}

// SetErrorVariable exposes a processing error to a recovery path as the
// structured message variable vars.error:
//
//	{ "message": <err.Error()>, "flow": <name>, "block": <failing block label> }
//
// name is the enclosing flow or block name. block is recovered from the error
// chain when the error originated in a leaf block, and is empty otherwise. It is
// a map[string]any so CEL expressions can read vars.error.message,
// vars.error.flow, and vars.error.block.
func SetErrorVariable(msg *types.Message, name string, err error) {
	msg.Variables.Set(ErrorVariable, map[string]any{
		"message": err.Error(),
		"flow":    name,
		"block":   FailingBlock(err),
	})
}
