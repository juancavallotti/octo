package agentmemory

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

// The context a flow forwards with an agent-memory call.
//
// Working memory, the recorded turns and user memories are written by a runtime
// on its own behalf — no block in the flow ever sees them — so a flow that wants
// this store to treat its memory differently has no other way to say so. The
// header is how it says so, per call, without the runtime storing anything.

// forwardedContextHeader carries it. The runtime writes it from
// core.MemoryContextHeader; the two are one wire contract implemented on both
// sides, because the runtime module is not a dependency of this one.
const forwardedContextHeader = "X-Octo-Agent-Context"

// forwardedContextLimit bounds what will be decoded. The header is a channel for
// a key or a tenant, not for data, and an unbounded base64 blob on a route that
// runs on every turn of every agent is worth refusing rather than parsing.
const forwardedContextLimit = 8 << 10

// forwardedContext reads what the flow forwarded with this request.
//
// No header is not an error: most calls forward nothing, and this returns an
// empty map for them so a reader never has to tell absent from empty.
//
// A header that is present and cannot be read IS an error. Our own runtime
// writes this header, so a value that does not decode means a caller meant to
// forward something and we did not get it — and the reason to forward anything
// is that the call cannot be served correctly without it. Reading that as "the
// flow forwarded nothing" would serve the call as if nobody had asked, which for
// a value like a key is the silent wrong answer rather than the loud one.
func forwardedContext(r *http.Request) (map[string]string, error) {
	value := r.Header.Get(forwardedContextHeader)
	if value == "" {
		return map[string]string{}, nil
	}
	if len(value) > forwardedContextLimit {
		return nil, fmt.Errorf("%w: %s is over %d bytes",
			ErrInvalidForwardedContext, forwardedContextHeader, forwardedContextLimit)
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not unpadded base64url",
			ErrInvalidForwardedContext, forwardedContextHeader)
	}
	var forwarded map[string]string
	if err := json.Unmarshal(raw, &forwarded); err != nil {
		return nil, fmt.Errorf("%w: %s does not hold a JSON object of strings",
			ErrInvalidForwardedContext, forwardedContextHeader)
	}
	if forwarded == nil {
		// JSON null unmarshals into a nil map without complaint, and nil is the shape
		// this promised never to return.
		return nil, fmt.Errorf("%w: %s holds null, not an object",
			ErrInvalidForwardedContext, forwardedContextHeader)
	}
	return forwarded, nil
}
