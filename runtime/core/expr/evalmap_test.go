package expr_test

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core/expr"
)

func compile(t *testing.T, expression string) *expr.Program {
	t.Helper()
	program, err := expr.CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	return program
}

// A nil program is an unset setting; a program that evaluates to the wrong kind
// is a mistake. The two are deliberately not the same answer, so an optional map
// can be left out without a null quietly standing in for one.
func TestEvalMapSeparatesUnsetFromWrongKind(t *testing.T) {
	got, err := expr.EvalMap(nil, nil)
	if err != nil || got != nil {
		t.Fatalf("nil program = (%v, %v), want (nil, nil)", got, err)
	}

	for _, expression := range []string{`null`, `"a string"`, `42`, `["a", "list"]`} {
		if _, err := expr.EvalMap(compile(t, expression), map[string]any{}); err == nil {
			t.Errorf("%s: want an error, got none", expression)
		} else if !strings.Contains(err.Error(), "want a map") {
			t.Errorf("%s: error = %v, want it to say what shape was expected", expression, err)
		}
	}
}

// Values are rendered the way EvalString renders a whole expression: a string
// verbatim, anything else as compact JSON. A map evaluated in one go has no
// per-value program to ask, so this is where that rule is applied.
func TestEvalStringMapRendersValues(t *testing.T) {
	program := compile(t, `{"text": "plain", "count": 3, "on": true, "nested": {"a": 1}, "list": [1, 2]}`)
	got, err := expr.EvalStringMap(program, map[string]any{})
	if err != nil {
		t.Fatalf("EvalStringMap: %v", err)
	}
	want := map[string]string{
		"text":   "plain",
		"count":  "3",
		"on":     "true",
		"nested": `{"a":1}`,
		"list":   "[1,2]",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q, want %q", name, got[name], value)
		}
	}
}

func TestEvalStringMapUnsetYieldsNoEntries(t *testing.T) {
	got, err := expr.EvalStringMap(nil, nil)
	if err != nil || got != nil {
		t.Fatalf("nil program = (%v, %v), want (nil, nil)", got, err)
	}
}
