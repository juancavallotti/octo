package alerting

import "time"

const (
	// resolveEvaluations is how many consecutive clean evaluations close an
	// incident.
	//
	// Deliberately asymmetric with the hold, and deliberately more than one. A
	// metric hovering at its threshold otherwise emits an open/resolve pair every
	// minute, and a stream of those is what teaches people to filter the alert
	// channel — at which point the watch that matters goes unread too.
	resolveEvaluations = 2

	// staleEvaluations is how many consecutive undecided evaluations close a
	// firing incident for lack of evidence.
	//
	// Counted in evaluations for the same reason the hold is: it is a number of
	// times we tried and could not tell, not a duration. Thirty of them is half an
	// hour at the default interval, which is long enough that a Redis restart does
	// not close an incident and short enough that a firing watch does not stay
	// firing forever on evidence nobody can refresh.
	staleEvaluations = 30
)

// Phase is where a watch's state machine has got to.
type Phase string

const (
	PhaseOK      Phase = "ok"
	PhasePending Phase = "pending"
	PhaseFiring  Phase = "firing"
	// PhaseInvalid is a watch whose stored definition will not build — most
	// likely one written by a newer version of this service. It is skipped rather
	// than evaluated, and it says so in the list, because evaluating a definition
	// this process only partly understands is worse than not evaluating it.
	PhaseInvalid Phase = "invalid"
)

// State is what survives a pod restart: one row per watch.
//
// It is in Postgres rather than in memory because the whole point of a hold is
// that it spans evaluations, and a process that restarted mid-incident must not
// re-open and re-announce one. It is not in Redis either — Redis here is a cache
// with TTLs, and alert state is the one thing in this feature that must not
// quietly expire.
type State struct {
	WatchID           string
	Phase             Phase
	Since             time.Time
	ConsecutiveFiring int
	ConsecutiveOK     int
	ConsecutiveErrors int
	DefinitionHash    string
	LastEvalAt        time.Time
	LastStatus        Status
	LastValue         *float64
	IncidentID        string
	LastNotifiedAt    time.Time
	MutedUntil        time.Time
	NextDueAt         time.Time
}

// ActionKind is what the state machine is asking the notifier to do.
type ActionKind string

const (
	ActionOpen     ActionKind = "open"     // the hold has been met: announce it
	ActionRenotify ActionKind = "renotify" // still firing, and the cooldown elapsed
	ActionResolve  ActionKind = "resolve"  // it recovered
	// ActionClose ends an episode that did not recover: the evidence ran out, or
	// somebody edited, disabled or deleted the watch underneath it. Distinct from
	// ActionResolve all the way into the incident row, because collapsing the two
	// is how a real outage gets recorded as fixed.
	ActionClose ActionKind = "close"
)

// Closed reasons, stored on the incident.
const (
	ClosedResolved = "resolved"
	ClosedStale    = "stale"
	ClosedEdited   = "edited"
	ClosedDisabled = "disabled"
	ClosedDeleted  = "deleted"
)

// Action is one thing to do about a transition.
type Action struct {
	Kind   ActionKind
	At     time.Time
	Reason string
}

// Step advances a watch's state by one evaluation.
//
// Pure: same inputs, same outputs, no clock of its own and nothing written. Every
// flapping rule this feature has lives in this one function, which is what lets a
// test drive twenty evaluations through it in a microsecond rather than needing a
// database and half an hour of wall time to find out what happens on the third.
func Step(cur State, w Watch, ev Evaluation, now time.Time) (State, []Action) {
	next := cur
	next.WatchID = w.ID
	next.LastEvalAt = now
	next.LastStatus = ev.Status
	next.LastValue = firstObserved(ev)
	next.NextDueAt = now.Add(w.Interval)

	// A definition change resets the machine before anything else looks at it.
	// The hold was measuring a different question, and carrying it across an edit
	// would fire on evidence gathered for a condition that no longer exists.
	var actions []Action
	if cur.DefinitionHash != w.DefinitionHash {
		next, actions = reset(next, w, now, ClosedEdited)
	}

	switch {
	case !ev.Decided():
		return undecided(next, w, now, actions)
	case ev.Firing():
		return firing(next, w, now, actions)
	default:
		return recovered(next, w, now, actions)
	}
}

// firing handles an evaluation whose combined verdict holds.
func firing(next State, w Watch, now time.Time, actions []Action) (State, []Action) {
	next.ConsecutiveFiring++
	next.ConsecutiveOK = 0
	next.ConsecutiveErrors = 0

	if next.ConsecutiveFiring < w.HoldEvaluations() {
		if next.Phase != PhasePending {
			next.Phase, next.Since = PhasePending, now
		}
		return next, actions
	}

	if next.Phase != PhaseFiring {
		next.Phase, next.Since = PhaseFiring, now
		next.IncidentID = "" // the store mints one and writes it back
		return next, append(actions, Action{Kind: ActionOpen, At: now})
	}
	if renotifyDue(next, w, now) {
		return next, append(actions, Action{Kind: ActionRenotify, At: now})
	}
	return next, actions
}

// recovered handles an evaluation whose verdict does not hold.
func recovered(next State, w Watch, now time.Time, actions []Action) (State, []Action) {
	next.ConsecutiveFiring = 0
	next.ConsecutiveErrors = 0
	next.ConsecutiveOK++

	switch next.Phase {
	case PhaseFiring:
		if next.ConsecutiveOK < resolveEvaluations {
			return next, actions
		}
		next.Phase, next.Since = PhaseOK, now
		return next, append(actions, Action{Kind: ActionResolve, At: now, Reason: ClosedResolved})
	case PhasePending:
		next.Phase, next.Since = PhaseOK, now
		return next, actions
	default:
		return next, actions
	}
}

// undecided handles an evaluation that could not reach a verdict.
//
// It breaks both streaks and advances neither, which is the only reading of
// "consecutive" that is consistent in both directions: a hold with a hole in it
// is not a hold, and neither is a recovery. It emphatically does not advance the
// clean streak, so an incident is never resolved by the absence of evidence — an
// outage marked fixed because the pipeline stopped reporting is the failure this
// rule exists to prevent.
//
// That does mean a firing watch whose backend is flaky can never resolve on its
// own, and that is the conservative direction rather than an oversight. The
// episode still ends: after the evidence has been missing long enough it closes
// as stale, which reads as what it was rather than as a recovery.
func undecided(next State, w Watch, now time.Time, actions []Action) (State, []Action) {
	next.ConsecutiveFiring = 0
	next.ConsecutiveOK = 0
	next.ConsecutiveErrors++

	switch next.Phase {
	case PhasePending:
		next.Phase, next.Since = PhaseOK, now
		return next, actions
	case PhaseFiring:
		if next.ConsecutiveErrors < staleEvaluations {
			return next, actions
		}
		next.Phase, next.Since = PhaseOK, now
		return next, append(actions, Action{Kind: ActionClose, At: now, Reason: ClosedStale})
	default:
		return next, actions
	}
}

// renotifyDue reports whether a still-firing watch should say so again.
func renotifyDue(s State, w Watch, now time.Time) bool {
	if w.Renotify <= 0 {
		return false
	}
	if s.LastNotifiedAt.IsZero() {
		return true
	}
	return !now.Before(s.LastNotifiedAt.Add(w.Renotify))
}

// reset returns the machine to rest, closing any open episode with the reason
// given. Used on a definition change, and by Disable and Delete below.
func reset(s State, w Watch, now time.Time, reason string) (State, []Action) {
	var actions []Action
	if s.Phase == PhaseFiring && s.IncidentID != "" {
		actions = append(actions, Action{Kind: ActionClose, At: now, Reason: reason})
	}
	s.Phase, s.Since = PhaseOK, now
	s.ConsecutiveFiring, s.ConsecutiveOK, s.ConsecutiveErrors = 0, 0, 0
	s.IncidentID = ""
	s.LastNotifiedAt = time.Time{}
	s.DefinitionHash = w.DefinitionHash
	return s, actions
}

// Retire closes a watch's episode because the watch itself is going away or being
// switched off. An incident that outlives its watch has nothing left that could
// ever resolve it, so this runs in the same transaction as the update.
func Retire(cur State, w Watch, now time.Time, reason string) (State, []Action) {
	return reset(cur, w, now, reason)
}

// Muted reports whether notifications are currently suppressed. The state machine
// still advances while a watch is muted — the history stays complete and an
// incident still opens and resolves — but nothing is sent, which is what somebody
// silencing a known-broken deployment for an afternoon actually wants.
func (s State) Muted(now time.Time) bool {
	return !s.MutedUntil.IsZero() && now.Before(s.MutedUntil)
}

// firstObserved is the number the list view shows beside a watch. The first
// condition's, because a composite has no single value and the alternative —
// showing none — leaves the row saying only that something is wrong.
func firstObserved(ev Evaluation) *float64 {
	for _, o := range ev.Outcomes {
		if o.Observed != nil {
			return o.Observed
		}
	}
	return nil
}
