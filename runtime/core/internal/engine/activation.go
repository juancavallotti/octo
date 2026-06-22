package engine

import "github.com/juancavallotti/octo/types"

// exprVarNames are the variable names every message-evaluated expression in the
// engine may reference: the decoded body, the message variables, the two
// identifiers, and the resolved environment. Blocks compile their expressions
// with these names and evaluate them against messageActivation so the expression
// surface stays uniform across the setters and the control-flow composites.
var exprVarNames = []string{"body", "vars", "eventID", "correlationID", "env"}

// messageActivation maps a message (and the block's resolved env) onto the
// variables an expression compiled with exprVarNames can reference.
func messageActivation(msg *types.Message, env map[string]any) map[string]any {
	return map[string]any{
		"body":          msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
		"env":           env,
	}
}

// envActivation materializes a resolved env map into the form CEL expects once
// at build time, so it can be shared across every message a block processes. A
// nil or empty env yields a non-nil empty map, keeping env.NAME a missing-key
// error rather than a null-deref.
func envActivation(env map[string]string) map[string]any {
	out := make(map[string]any, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}
