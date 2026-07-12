package expr

import "github.com/juancavallotti/octo/core"

// SourcePayloadVars are the variables a source's payload expression may reference
// when it builds the initial message: the trigger time and the source's static
// settings. A source has produced no message yet, so the message variables
// (MessageVars) are not in scope.
var SourcePayloadVars = []string{nowVar, "settings"}

// CompileSourcePayload compiles a source payload expression with SourcePayloadVars
// and every registered message extension, so a capability wired in through
// RegisterMessageExtension — templateResource(id) among them — works in a payload
// exactly as it does in a message expression, resolved against the source's own
// scope. res may be nil (a no-op loader is used).
func CompileSourcePayload(res core.ResourceLoader, expression string) (*Program, error) {
	return compileWithExtensions(res, SourcePayloadVars, expression)
}
