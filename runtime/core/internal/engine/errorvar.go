package engine

import (
	"errors"

	"github.com/juancavallotti/octo/types"
)

// errorVarName is the message variable under which a processing error is exposed
// to recovery paths (the flow-level error chain and the handle-errors block).
const errorVarName = "error"

// SetErrorVariable exposes a processing error to a recovery path as the structured
// message variable vars.error:
//
//	{ "message": <err.Error()>, "flow": <name>, "block": <failing block label> }
//
// name is the enclosing flow or handle-errors block name. block is recovered from
// the error chain via errors.As when the error originated in a leaf block, and is
// empty otherwise. It is a map[string]any so CEL expressions can read
// vars.error.message, vars.error.flow, and vars.error.block.
func SetErrorVariable(msg *types.Message, name string, err error) {
	block := ""
	var be *blockError
	if errors.As(err, &be) {
		block = be.label
	}
	msg.Variables.Set(errorVarName, map[string]any{
		"message": err.Error(),
		"flow":    name,
		"block":   block,
	})
}
