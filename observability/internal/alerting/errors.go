package alerting

import "errors"

// The sentinel errors this package returns. They are values rather than strings
// because callers branch on several of them: the store marks a watch invalid when
// a definition will not decode, and the runner records a fetch failure as a
// degraded evaluation rather than losing the tick.
var (
	// ErrNoSamples is a window with nothing known in it. Not a failure — it is the
	// ordinary answer for a quiet app, and it is what separates "insufficient"
	// from "ok" in the history.
	ErrNoSamples = errors.New("no samples in the window")

	// ErrNotReducible is an aggregate that cannot collapse a slice of bucket
	// values, which today means a ratio.
	ErrNotReducible = errors.New("aggregate is not reducible over bucket values")

	ErrUnknownAggregate = errors.New("unknown aggregate")
	ErrUnknownSource    = errors.New("unknown source")
	ErrUnknownCondition = errors.New("unknown condition type")
	ErrUnknownAction    = errors.New("unknown action type")

	// ErrNestedConditions is a group inside a group. The stored shape can carry
	// one, so that nesting can be added later without moving a row, but nothing
	// evaluates it yet — and a definition that silently evaluated part of itself
	// is the failure this refuses.
	ErrNestedConditions = errors.New("nested condition groups are not supported")

	ErrInvalidParams  = errors.New("invalid condition parameters")
	ErrInvalidWatch   = errors.New("invalid watch")
	ErrTooManyWatches = errors.New("too many watches")
	ErrWatchNotFound  = errors.New("watch not found")
	ErrNameTaken      = errors.New("a watch with that name already exists")
)
