package embedding

import "errors"

// ErrNotConfigured means this installation has no embedding server.
//
// It is not a failure. Agent memory works without one — search matches text
// rather than ranking by meaning — so callers treat this as "skip the vector
// half", not as an error to report. The sweep stops asking; search falls back.
var ErrNotConfigured = errors.New("embedding: no embedding server is configured")
