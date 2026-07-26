package types

import "encoding/json"

// copyBody returns a deep copy of a message body.
//
// Body is decoded JSON by the type's contract, so the copy is a structural walk
// of the two container kinds — map[string]any and []any — with the scalar kinds
// shared by value because they are immutable. Nothing is serialized, which is
// what makes copying a large body cheap: the cost is the allocations the new
// containers need and nothing else.
//
// Anything off that contract (a Go struct, an int, a time.Time) falls back to a
// JSON round-trip for that subtree alone. That both copies it and normalizes it
// to the kinds the contract promises — numbers float64, objects map[string]any,
// arrays []any — exactly as the whole body used to be handled.
//
// A value that will not round-trip either — a channel, a func — is handed back
// as-is, so that part stays shared with the original. Losing the body would be
// worse than aliasing it, and such a value is a contract violation to begin with:
// a body is JSON, and every body the runtime produces already is.
func copyBody(body any) any {
	switch typed := body.(type) {
	case nil:
		return nil
	case string, float64, bool:
		// Immutable: sharing is copying.
		return body
	case map[string]any:
		if typed == nil {
			// A round-trip renders a nil map as null and decodes it back to an
			// untyped nil; match that rather than inventing an empty map.
			return nil
		}
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = copyBody(v)
		}
		return out
	case []any:
		if typed == nil {
			return nil
		}
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = copyBody(v)
		}
		return out
	default:
		return jsonCopy(body)
	}
}

// jsonCopy deep-copies a value through a JSON round-trip, normalizing it to
// decoded-JSON kinds on the way. It is the fallback for values that break the
// body contract, and returns the value untouched when it will not encode.
//
// The structural walk above can recurse forever on a cycle through map[string]any
// or []any; only this path turns one into an error instead. Bodies come from a
// JSON decode or a CEL result, so neither can be cyclic today.
func jsonCopy(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return value
	}
	return decoded
}
