package engine

import "github.com/juancavallotti/eip-go/types"

// exprVarNames are the variable names every message-evaluated expression in the
// engine may reference: the decoded body, the message variables, and the two
// identifiers. Blocks compile their expressions with these names and evaluate
// them against messageActivation so the expression surface stays uniform across
// the setters and the control-flow composites.
var exprVarNames = []string{"body", "vars", "eventID", "correlationID"}

// messageActivation maps a message onto the variables an expression compiled
// with exprVarNames can reference.
func messageActivation(msg *types.Message) map[string]any {
	return map[string]any{
		"body":          msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
	}
}
