package expr

import (
	"reflect"
	"testing"
)

func TestToYAML(t *testing.T) {
	prog, err := CompileMessage(nil, `toYaml(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(map[string]any{"name": "Ann", "count": 2}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	// The structpb bridge sorts object keys, so the rendering is deterministic.
	if want := "count: 2\nname: Ann\n"; out != want {
		t.Errorf("toYaml = %q, want %q", out, want)
	}
}

// TestToYAMLQuotesAmbiguousScalars pins the behavior that makes the round trip
// safe: a bare "n" or "yes" reads as a boolean under YAML 1.1, so yaml.v3 quotes
// any string that would otherwise come back as a different type.
func TestToYAMLQuotesAmbiguousScalars(t *testing.T) {
	prog, err := CompileMessage(nil, `toYaml(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(map[string]any{"n": "yes", "version": "1.0"}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	if want := "\"n\": \"yes\"\nversion: \"1.0\"\n"; out != want {
		t.Errorf("toYaml = %q, want %q", out, want)
	}
}

func TestFromYAML(t *testing.T) {
	prog, err := CompileMessage(nil, `fromYaml(body).name`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation("name: Ann\n"))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if out != "Ann" {
		t.Errorf("fromYaml(body).name = %v, want Ann", out)
	}
}

// TestFromYAMLDecodesJSONNativeShapes is the reason fromYaml is not a copy of
// fromJson: YAML decodes an integer as int and a timestamp as time.Time, and a
// message body may hold neither. Every scalar here must arrive as the JSON-native
// type a body is allowed to carry.
func TestFromYAMLDecodesJSONNativeShapes(t *testing.T) {
	const doc = `
count: 3
ratio: 1.5
enabled: true
missing: null
name: Ann
when: 2026-08-15T00:00:00Z
tags:
  - a
  - b
nested:
  k: v
`
	prog, err := CompileMessage(nil, `fromYaml(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation(doc))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := map[string]any{
		"count":   float64(3),
		"ratio":   1.5,
		"enabled": true,
		"missing": nil,
		"name":    "Ann",
		// A YAML timestamp decodes to time.Time; normalizing through JSON renders
		// it as its RFC 3339 string, which is what a body may hold.
		"when":   "2026-08-15T00:00:00Z",
		"tags":   []any{"a", "b"},
		"nested": map[string]any{"k": "v"},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("fromYaml = %#v, want %#v", out, want)
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	prog, err := CompileMessage(nil, `fromYaml(toYaml(body))`)
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

func TestFromYAMLErrors(t *testing.T) {
	tests := []struct {
		name string
		body any
	}{
		{name: "malformed document", body: "a:\n\t- b"},
		{name: "non-string mapping key", body: "? [1, 2]\n: value"},
		{name: "argument is not a string", body: map[string]any{"a": 1}},
	}
	prog, err := CompileMessage(nil, `fromYaml(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := prog.Eval(messageActivation(tc.body)); err == nil {
				t.Fatal("expected an evaluation error")
			}
		})
	}
}
