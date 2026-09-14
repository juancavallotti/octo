package agentmemory

import "errors"

var (
	// ErrVersionConflict is returned by a write whose expected version does not
	// match the stored row, so the caller re-reads and retries. The handler maps it
	// to 409 Conflict, and the runtime's client maps that back to
	// core.ErrVersionConflict.
	ErrVersionConflict = errors.New("agent memory: version conflict")
	// ErrNotFound is returned for a conversation that does not exist. The handler
	// maps it to 404.
	ErrNotFound = errors.New("agent memory: not found")
	// ErrInvalidForwardedContext is returned for a forwarded-context header that is
	// present and cannot be read. The handler maps it to 400.
	//
	// It is separate from "no header at all", which is not an error and is what
	// most calls look like. A header that arrived mangled means a caller meant to
	// forward something and this service did not get it, and the whole reason to
	// forward anything is that the call cannot be served correctly without it.
	ErrInvalidForwardedContext = errors.New("agent memory: unreadable forwarded context")
	// ErrInvalidRef is returned for an agent id, thread key or user id that cannot
	// be stored — empty, too long, or carrying control characters. Refused rather
	// than trimmed: a key a caller reads back must be the one it wrote.
	ErrInvalidRef = errors.New("agent memory: invalid identifier")
)
