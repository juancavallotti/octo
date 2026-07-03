package expr

// SourcePayloadVars are the variables a source's payload expression may reference
// when it builds the initial message: the trigger time and the source's static
// settings. A source has produced no message yet, so the message variables
// (MessageVars) are not in scope.
var SourcePayloadVars = []string{"now", "settings"}

// CompileSourcePayload compiles a source payload expression with SourcePayloadVars.
// It is the single entry point for source payload expressions, keeping their
// variable set defined in one place rather than named inline at each source.
func CompileSourcePayload(expression string) (*Program, error) {
	return CompileWithOptions(expression, SourcePayloadVars)
}
