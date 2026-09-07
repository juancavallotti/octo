package alerting

import "time"

const (
	// DefaultHistoryLimit and MaxHistoryLimit bound one page of evaluation
	// history, matching what the logs and traces endpoints already use so the
	// three list views page alike.
	DefaultHistoryLimit = 100
	MaxHistoryLimit     = 1000
)

// Result is everything one evaluation produced: what was decided, where that
// left the state machine, and what it asked to be done about it.
//
// It is written in one transaction. A history row without its state update would
// leave a watch re-evaluating a window it has already judged; a state update
// without its history row would leave an incident nothing explains.
type Result struct {
	Watch      Watch
	Evaluation Evaluation
	Previous   State
	State      State
	Actions    []Action
	Duration   time.Duration
}

// Transitioned reports whether this evaluation moved the state machine, which is
// what the history row records so "show me only the interesting ones" does not
// have to compare adjacent rows.
func (r Result) Transitioned() bool { return r.Previous.Phase != r.State.Phase }

// Due is a watch together with where its state machine got to: what the
// scheduler hands the runner, and what the list view renders.
//
// It lives here rather than in the store package because the runner consumes it
// and the runner may not import the store — the dependency runs the other way,
// so that the alerting package's test binary links no database driver.
type Due struct {
	Watch Watch
	State State
}

// Incident is one firing episode.
//
// The numbers that opened it are frozen onto the row. A watch can be edited
// mid-incident, and an incident page that recomputed its own trigger against the
// edited definition would describe something that never happened.
type Incident struct {
	ID             string
	WatchID        string
	WatchName      string
	OpenedAt       time.Time
	ResolvedAt     *time.Time
	ClosedReason   string
	Severity       string
	AcknowledgedAt *time.Time
	AcknowledgedBy string
	OpenedMatched  int
	OpenedTotal    int
	OpenedOutcomes []Outcome
	Evaluations    int
	Notifications  int
}

// Open reports whether the episode is still running.
func (i Incident) Open() bool { return i.ResolvedAt == nil }

// EvaluationRecord is one history row as it is read back.
type EvaluationRecord struct {
	ID            string
	WatchID       string
	IncidentID    string
	EvaluatedAt   time.Time
	Status        Status
	Phase         Phase
	PreviousPhase Phase
	Transitioned  bool
	Degraded      bool
	Matched       int
	Total         int
	WindowFrom    *time.Time
	WindowTo      *time.Time
	Reason        string
	Err           string
	DurationMS    int
	Outcomes      []Outcome
}

// HistoryCursor is the keyset a history page resumes from. Composite, because a
// tick evaluates every due watch inside the same millisecond and a cursor on the
// timestamp alone would skip or repeat rows that tie across a page boundary.
type HistoryCursor struct {
	At time.Time
	ID string
}

// HistoryFilter narrows the evaluation log. A zero field is no constraint.
type HistoryFilter struct {
	WatchID    string
	IncidentID string
	Statuses   []Status
	// NotableOnly drops the rows that say nothing happened, which is the default
	// view of a table where the overwhelming majority of rows are exactly that.
	NotableOnly bool
	From, To    *time.Time
	Before      *HistoryCursor
	Limit       int
}

// Clamp bounds the page size, so a caller asking for everything gets a page.
func (f HistoryFilter) Clamp() int {
	switch {
	case f.Limit <= 0:
		return DefaultHistoryLimit
	case f.Limit > MaxHistoryLimit:
		return MaxHistoryLimit
	default:
		return f.Limit
	}
}

// IncidentFilter narrows the incident list.
type IncidentFilter struct {
	WatchID  string
	OpenOnly bool
	From, To *time.Time
	Limit    int
}
