package expr

import (
	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// ToolCallVars are the variables an expression about a tool call may reference:
// everything a message expression sees, plus the call itself.
//
// The call is split in two on purpose. `tool` is who is being called — a name and
// the provider's id for this invocation — and `input` is what it was asked to do,
// decoded, so a condition reads `input.method != "GET"` rather than parsing a JSON
// string. That split is the difference between a rule about a verb and a rule
// about an argument, which is the distinction an authorization exists to make.
var ToolCallVars = unionVars(MessageVars, []string{"tool", "input"})

// CompileToolCall compiles an expression evaluated against one tool call, with
// ToolCallVars in scope and every registered message extension applied — so a
// capability wired in through RegisterMessageExtension works here exactly as it
// does in a message expression. res may be nil (a no-op loader is used).
func CompileToolCall(res core.ResourceLoader, expression string) (*Program, error) {
	return compileWithExtensions(MessageContext{Resources: res, Vars: ToolCallVars}, expression)
}

// ToolCallActivation maps a message and the call the model asked for onto the
// ToolCallVars a tool-call expression references. It is the single definition of
// that activation shape, paired with ToolCallVars the way MessageActivation is
// paired with MessageVars.
//
// input is the decoded arguments, or nil when they are absent or not JSON — a
// condition reading a field of nothing fails loudly rather than quietly deciding
// no authorization was needed.
func ToolCallActivation(
	msg *types.Message, env map[string]any, name, id string, input any,
) map[string]any {
	activation := MessageActivation(msg, env)
	activation["tool"] = map[string]any{"name": name, "id": id}
	activation["input"] = input
	return activation
}
