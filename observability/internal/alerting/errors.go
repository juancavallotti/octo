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

	// ErrStaleEvaluation is a write refused because a newer decision is already
	// recorded — an evaluator whose lease has moved to another pod. Losing that
	// race is logged and dropped, never retried: the newer decision is the one
	// worth keeping.
	ErrStaleEvaluation = errors.New("a newer evaluation is already recorded")

	ErrIncidentNotFound = errors.New("incident not found")

	// ErrCoarseData is a fetch answered at a coarser step than the condition
	// asked for.
	//
	// An error rather than a sparse series, because afterwards the two cannot be
	// told apart. A tier whose step is ten minutes, re-bucketed onto thirty-second
	// buckets, yields one value and nineteen empty ones — which is exactly what a
	// series that stopped reporting looks like, and is read as silence by the one
	// condition whose whole job is to detect silence. Refusing to answer is the
	// only reply that cannot be mistaken for an observation.
	ErrCoarseData = errors.New("data is not retained at this resolution")

	ErrInvalidParams  = errors.New("invalid condition parameters")
	ErrInvalidWatch   = errors.New("invalid watch")
	ErrTooManyWatches = errors.New("too many watches")
	ErrWatchNotFound  = errors.New("watch not found")
	ErrNameTaken      = errors.New("a watch with that name already exists")
)
