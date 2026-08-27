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
	// ErrInvalidRef is returned for an agent id, thread key or user id that cannot
	// be stored — empty, too long, or carrying control characters. Refused rather
	// than trimmed: a key a caller reads back must be the one it wrote.
	ErrInvalidRef = errors.New("agent memory: invalid identifier")
)
