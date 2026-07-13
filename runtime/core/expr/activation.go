package expr

import (
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// MessageActivation maps a message and its resolved env onto the variables a
// message expression (compiled via CompileMessage, which declares MessageVars) can
// reference. It is the single definition of the activation shape: blocks and
// connectors build their activation through it rather than assembling the map with
// literal keys, so MessageVars and this function never drift apart. now is the
// evaluation time (use string(now) to render it in a JSON body).
func MessageActivation(msg *types.Message, env map[string]any) map[string]any {
	return map[string]any{
		"body":          msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
		"env":           env,
		nowVar:          time.Now(),
	}
}

// EnvActivation materializes a resolved env map into the form CEL expects once at
// build time, so it can be shared across every message a block processes. A nil or
// empty env yields a non-nil empty map, keeping env.NAME a missing-key error
// rather than a null-deref.
func EnvActivation(env map[string]string) map[string]any {
	out := make(map[string]any, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

// SourcePayloadActivation maps a source's fire time and static settings onto the
// SourcePayloadVars a source payload expression references. It is the single
// definition of the source-payload activation shape (paired with SourcePayloadVars).
func SourcePayloadActivation(settings map[string]any) map[string]any {
	return map[string]any{
		nowVar:     time.Now(),
		"settings": settings,
	}
}
