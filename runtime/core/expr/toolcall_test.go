package expr_test

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func toolCallMessage(t *testing.T) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Variables.Set("caller", "dr-octo")
	return msg
}

func TestCompileToolCallReadsTheArguments(t *testing.T) {
	program, err := expr.CompileToolCall(nil, `tool.name == "octo_api" && input.method != "GET"`)
	if err != nil {
		t.Fatalf("CompileToolCall: %v", err)
	}
	msg := toolCallMessage(t)

	cases := []struct {
		name  string
		tool  string
		input any
		want  bool
	}{
		{"a write on the gated tool", "octo_api", map[string]any{"method": "PUT"}, true},
		{"a read on the gated tool", "octo_api", map[string]any{"method": "GET"}, false},
		{"a write on another tool", "octo_read", map[string]any{"method": "PUT"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, evalErr := program.Eval(
				expr.ToolCallActivation(msg, nil, tc.tool, "call-1", tc.input))
			if evalErr != nil {
				t.Fatalf("Eval: %v", evalErr)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompileToolCallSeesTheMessageScope(t *testing.T) {
	program, err := expr.CompileToolCall(nil, `vars.caller + "/" + tool.id`)
	if err != nil {
		t.Fatalf("CompileToolCall: %v", err)
	}
	got, err := program.Eval(expr.ToolCallActivation(toolCallMessage(t), nil, "octo_api", "call-7", nil))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != "dr-octo/call-7" {
		t.Fatalf("got %v, want dr-octo/call-7", got)
	}
}
