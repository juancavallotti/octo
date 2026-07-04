package expr

import (
	"reflect"
	"testing"
)

func TestToJSON(t *testing.T) {
	prog, err := CompileMessage(nil, `toJson(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(map[string]any{"name": "Ann", "n": 2}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	// Object key order is stable in encoding/json (sorted), so this is exact.
	if out != `{"n":2,"name":"Ann"}` {
		t.Errorf("toJson = %q, want %q", out, `{"n":2,"name":"Ann"}`)
	}
}

func TestFromJSON(t *testing.T) {
	prog, err := CompileMessage(nil, `fromJson(body).name`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation(`{"name":"Ann"}`))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if out != "Ann" {
		t.Errorf("fromJson(body).name = %v, want Ann", out)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	prog, err := CompileMessage(nil, `fromJson(toJson(body))`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	body := map[string]any{"name": "Ann", "tags": []any{"a", "b"}, "nested": map[string]any{"k": "v"}}
	out, err := prog.Eval(messageActivation(body))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := map[string]any{"name": "Ann", "tags": []any{"a", "b"}, "nested": map[string]any{"k": "v"}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("round-trip = %#v, want %#v", out, want)
	}
}

func TestFromJSONInvalid(t *testing.T) {
	prog, err := CompileMessage(nil, `fromJson(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	if _, err := prog.Eval(messageActivation("{not json")); err == nil {
		t.Fatal("expected an error decoding malformed JSON")
	}
}
