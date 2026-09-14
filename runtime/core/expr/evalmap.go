package expr

import (
	"encoding/json"
	"fmt"
)

// EvalMap evaluates an expression against the activation, erroring if the result
// is not a map. It is the one evaluator every setting that takes a whole object
// shares — a rest-dynamic header map, a task's metadata, a Pinecone filter — so
// "want a map" is worded once.
//
// A nil program is an unset setting and yields no map at all, which is what lets
// a caller hold an optional expression without guarding every use. A program that
// evaluates to null is not the same thing: the author wrote an expression and it
// produced the wrong kind, so it is reported rather than read as absence.
func EvalMap(program *Program, activation map[string]any) (map[string]any, error) {
	if program == nil {
		return nil, nil
	}
	value, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expression produced %T, want a map", value)
	}
	return result, nil
}

// EvalStringMap is EvalMap for the maps that go on a wire, rendering each value
// the way Program.EvalString renders a whole expression: a string verbatim,
// anything else as compact JSON. It exists because a setting that evaluates a
// whole map at once has no per-value program to call EvalString on.
func EvalStringMap(program *Program, activation map[string]any) (map[string]string, error) {
	entries, err := EvalMap(program, activation)
	if err != nil || entries == nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for name, raw := range entries {
		rendered, err := renderValue(raw)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", name, err)
		}
		out[name] = rendered
	}
	return out, nil
}

// renderValue turns one evaluated value into the string that goes on the wire.
func renderValue(value any) (string, error) {
	if s, ok := value.(string); ok {
		return s, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode value: %w", err)
	}
	return string(raw), nil
}
