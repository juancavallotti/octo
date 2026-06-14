package expr

import (
	"encoding/json"
	"testing"
)

func TestEvalStringConcatenation(t *testing.T) {
	program, err := Compile(`"order " + body.id`, "body")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := program.EvalString(map[string]any{"body": map[string]any{"id": "42"}})
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	if got != "order 42" {
		t.Errorf("EvalString = %q, want %q", got, "order 42")
	}
}

// TestEvalStringRendersObjectAsJSON covers the non-string path EvalString uses
// and, with it, that a CEL map value round-trips through encoding/json — the
// same conversion the cron source relies on for object payloads.
func TestEvalStringRendersObjectAsJSON(t *testing.T) {
	program, err := Compile(`{"kind": "tick", "n": 1}`, "now")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := program.EvalString(map[string]any{"now": "ignored"})
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("result %q is not valid JSON: %v", got, err)
	}
	if decoded["kind"] != "tick" {
		t.Errorf("decoded kind = %v, want tick", decoded["kind"])
	}
}

func TestCompileRejectsBadExpression(t *testing.T) {
	if _, err := Compile("body.", "body"); err == nil {
		t.Fatal("expected a compile error for a malformed expression")
	}
}

func TestEvalUnboundVariableErrors(t *testing.T) {
	program, err := Compile("body.id", "body")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := program.Eval(map[string]any{}); err == nil {
		t.Fatal("expected an evaluation error for an unbound variable")
	}
}
