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
func copyBody(body any) any { return copyBodyAt(body, 0) }

// maxCopyDepth bounds the structural walk. encoding/json's own scanner refuses to
// decode past this nesting, so no decoded body can reach it: a value that does is
// not a decoded body but a cycle built out of contract-conforming containers —
// m["self"] = m, which a block can construct even though no JSON decode ever
// produces one. The walk would chase that until the stack gave out, which is fatal
// and unrecoverable, so the cap hands it to the round-trip instead: that path
// detects the cycle, fails to encode, and returns the value shared rather than
// copied. Aliasing a body a block should not have built beats killing the process.
const maxCopyDepth = 10000

// copyBodyAt is copyBody carrying the depth reached so far.
func copyBodyAt(body any, depth int) any {
	if depth > maxCopyDepth {
		return jsonCopy(body)
	}
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
			out[k] = copyBodyAt(v, depth+1)
		}
		return out
	case []any:
		if typed == nil {
			return nil
		}
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = copyBodyAt(v, depth+1)
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
// It is also where a cycle through map[string]any or []any lands, once the walk
// above hits its depth cap: this is the only path that reports one rather than
// following it. Bodies come from a JSON decode or a CEL result, so neither can be
// cyclic today.
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
