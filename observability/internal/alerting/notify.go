package alerting

import "time"

// The action types a watch may carry. Closed, and an unrecognised one is an
// error at save time on the same terms a condition type is: an action silently
// skipped is an alert that fired and told nobody.
const (
	ActionTypeTopic = "topic"
	ActionTypeEmail = "email"
	ActionTypeLog   = "log"
)

// MaxActions bounds one watch. Generous next to the condition limit, because
// fanning one alert out to a few places is ordinary where asking ten questions at
// once is not.
const MaxActions = 10

// Notification is what an action delivers: everything a reader needs to know
// what happened, without having to look anything up.
//
// It is built from the evaluation that caused it rather than read back from the
// database, so the numbers in an email are the numbers that fired it — a
// notification that re-queried would describe the state at the moment it was
// sent, which is not the same fact and is worse for having looked authoritative.
type Notification struct {
	Kind       ActionKind `json:"kind"`
	At         time.Time  `json:"at"`
	WatchID    string     `json:"watchId"`
	WatchName  string     `json:"watchName"`
	Severity   string     `json:"severity"`
	IncidentID string     `json:"incidentId,omitempty"`
	// Reason is set on the closing kinds, and is what keeps "it recovered" apart
	// from "we stopped being able to tell".
	Reason string `json:"reason,omitempty"`

	Combinator Combinator `json:"combinator"`
	Matched    int        `json:"matched"`
	Total      int        `json:"total"`
	Degraded   bool       `json:"degraded,omitempty"`
	Outcomes   []Outcome  `json:"outcomes"`

	WindowFrom time.Time `json:"windowFrom"`
	WindowTo   time.Time `json:"windowTo"`
}

// NewNotification builds the payload for one action.
func NewNotification(w Watch, state State, ev Evaluation, action Action) Notification {
	return Notification{
		Kind:       action.Kind,
		At:         action.At,
		WatchID:    w.ID,
		WatchName:  w.Name,
		Severity:   w.Severity,
		IncidentID: state.IncidentID,
		Reason:     action.Reason,
		Combinator: ev.Combinator,
		Matched:    ev.Matched,
		Total:      ev.Total,
		Degraded:   ev.Degraded,
		Outcomes:   ev.Outcomes,
		WindowFrom: ev.WindowFrom,
		WindowTo:   ev.WindowTo,
	}
}

// Resolved reports whether this notification is announcing an end rather than a
// beginning, which is what decides the wording every action puts around it.
func (n Notification) Resolved() bool {
	return n.Kind == ActionResolve || n.Kind == ActionClose
}

// Headline is the one-line summary every action leads with.
func (n Notification) Headline() string {
	switch n.Kind {
	case ActionResolve:
		return n.WatchName + " recovered"
	case ActionClose:
		return n.WatchName + " closed (" + n.Reason + ")"
	case ActionRenotify:
		return n.WatchName + " is still firing"
	default:
		return n.WatchName + " is firing"
	}
}
