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
// copied is false when some part of the value would not round-trip. The original
// is handed back for that part, still shared, because there is no copy to be had
// and losing the body would be worse than aliasing it. It is reported all the way
// up: a container holding one uncopyable leaf is itself only partly copied, and a
// caller that writes in place needs to know.
func copyBody(body any) (value any, copied bool) {
	switch typed := body.(type) {
	case nil:
		return nil, true
	case string, float64, bool:
		// Immutable: sharing is copying.
		return body, true
	case map[string]any:
		if typed == nil {
			// A round-trip renders a nil map as null and decodes it back to an
			// untyped nil; match that rather than inventing an empty map.
			return nil, true
		}
		out := make(map[string]any, len(typed))
		copied = true
		for k, v := range typed {
			child, childCopied := copyBody(v)
			out[k] = child
			copied = copied && childCopied
		}
		return out, copied
	case []any:
		if typed == nil {
			return nil, true
		}
		out := make([]any, len(typed))
		copied = true
		for i, v := range typed {
			child, childCopied := copyBody(v)
			out[i] = child
			copied = copied && childCopied
		}
		return out, copied
	default:
		return jsonCopy(body)
	}
}

// jsonCopy deep-copies a value through a JSON round-trip, normalizing it to
// decoded-JSON kinds on the way. It is the fallback for values that break the
// body contract. Structural copy paths can recurse on cyclic map[string]any or
// []any values, while jsonCopy reports cycles encountered during JSON encoding.
func jsonCopy(value any) (any, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return value, false
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return value, false
	}
	return decoded, true
}
