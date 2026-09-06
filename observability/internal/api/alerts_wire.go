package api

import (
	"encoding/json"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// The wire shapes for alerting.
//
// Separate from the handler because there are a lot of them and they are all
// mapping, and because the mapping is the part the platform's own client mirrors
// field for field. snake_case, like every other response this service serves.

// watchBody is a watch as it is written and read back.
//
// Durations are seconds rather than Go duration strings: the form holds numbers,
// the column holds seconds, and a string in between would be a third
// representation that only exists on the wire.
type watchBody struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Severity    string `json:"severity"`

	Combinator string `json:"combinator"`
	// Conditions and Actions travel as the open objects they are stored as,
	// parameters and all.
	//
	// Deliberately untyped here rather than a struct per kind: describing them
	// field by field would mean this package knowing every condition's
	// parameters, which is the one thing a discriminated shape exists to avoid,
	// and it would put a second definition of them beside the one the domain
	// already decodes strictly. The domain's own builder is what refuses a
	// malformed one, and it does so with a message naming the field.
	Conditions []map[string]any `json:"conditions"`
	Actions    []map[string]any `json:"actions"`
	OnNoData   string           `json:"on_no_data"`

	StepSeconds     int `json:"step_seconds"`
	IntervalSeconds int `json:"interval_seconds"`
	ForSeconds      int `json:"for_seconds"`
	RenotifySeconds int `json:"renotify_seconds"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// watchStateBody is the machine's position, rendered beside a watch in the list.
type watchStateBody struct {
	Phase             string     `json:"phase"`
	Since             *time.Time `json:"since"`
	ConsecutiveFiring int        `json:"consecutive_firing"`
	ConsecutiveOK     int        `json:"consecutive_ok"`
	LastEvalAt        *time.Time `json:"last_eval_at"`
	LastStatus        string     `json:"last_status"`
	LastValue         *float64   `json:"last_value"`
	IncidentID        string     `json:"incident_id,omitempty"`
	MutedUntil        *time.Time `json:"muted_until"`
	NextDueAt         *time.Time `json:"next_due_at"`
}

// watchListItem is one row of the list: the definition and where it has got to.
type watchListItem struct {
	Watch watchBody      `json:"watch"`
	State watchStateBody `json:"state"`
}

type watchListResponse struct {
	Items []watchListItem `json:"items"`
}

// incidentBody is one firing episode.
type incidentBody struct {
	ID             string     `json:"id"`
	WatchID        string     `json:"watch_id"`
	WatchName      string     `json:"watch_name"`
	OpenedAt       time.Time  `json:"opened_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	ClosedReason   string     `json:"closed_reason,omitempty"`
	Severity       string     `json:"severity"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	OpenedMatched  int        `json:"opened_matched"`
	OpenedTotal    int        `json:"opened_total"`
	// The outcomes that opened it, frozen. A watch can be retuned mid-incident,
	// and a page that recomputed its own trigger would describe something that
	// never happened.
	OpenedOutcomes []alerting.Outcome `json:"opened_outcomes"`
	Evaluations    int                `json:"evaluations"`
	Notifications  int                `json:"notifications"`
}

type incidentListResponse struct {
	Items []incidentBody `json:"items"`
}

// evaluationBody is one row of the execution log.
type evaluationBody struct {
	ID            string             `json:"id"`
	WatchID       string             `json:"watch_id"`
	IncidentID    string             `json:"incident_id,omitempty"`
	EvaluatedAt   time.Time          `json:"evaluated_at"`
	Status        string             `json:"status"`
	Phase         string             `json:"phase"`
	PreviousPhase string             `json:"previous_phase"`
	Transitioned  bool               `json:"transitioned"`
	Degraded      bool               `json:"degraded"`
	Matched       int                `json:"matched"`
	Total         int                `json:"total"`
	WindowFrom    *time.Time         `json:"window_from"`
	WindowTo      *time.Time         `json:"window_to"`
	Reason        string             `json:"reason,omitempty"`
	Error         string             `json:"error,omitempty"`
	DurationMs    int                `json:"duration_ms"`
	Outcomes      []alerting.Outcome `json:"outcomes"`
}

// evaluationListResponse is one page of history. NextBefore is opaque and names a
// (evaluated_at, id) pair, because a tick evaluates every due watch inside the
// same millisecond and a timestamp alone cannot name a row.
type evaluationListResponse struct {
	Items      []evaluationBody `json:"items"`
	NextBefore string           `json:"next_before,omitempty"`
}

// previewResponse is what a definition would have decided right now.
type previewResponse struct {
	Status     string             `json:"status"`
	Verdict    string             `json:"verdict"`
	Matched    int                `json:"matched"`
	Total      int                `json:"total"`
	Degraded   bool               `json:"degraded"`
	WindowFrom time.Time          `json:"window_from"`
	WindowTo   time.Time          `json:"window_to"`
	Outcomes   []alerting.Outcome `json:"outcomes"`
}

type muteRequest struct {
	// Until is when the mute lifts. Null lifts it now, which is how somebody
	// un-mutes without having to pick a time in the past.
	Until *time.Time `json:"until"`
}

func toWatchBody(w alerting.Watch) watchBody {
	return watchBody{
		ID: w.ID, Name: w.Name, Description: w.Description, Enabled: w.Enabled,
		Severity: w.Severity, Combinator: string(w.Combinator),
		Conditions: rawConditions(w.Conditions), Actions: rawActions(w.Actions),
		OnNoData:        string(w.OnNoData),
		StepSeconds:     int(w.Step.Seconds()),
		IntervalSeconds: int(w.Interval.Seconds()),
		ForSeconds:      int(w.For.Seconds()),
		RenotifySeconds: int(w.Renotify.Seconds()),
		CreatedAt:       nonZero(w.CreatedAt), UpdatedAt: nonZero(w.UpdatedAt),
	}
}

// rawConditions renders the specs as open objects, so the wire carries exactly
// the shape that was stored.
func rawConditions(in []alerting.ConditionSpec) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, c := range in {
		if obj, err := toObject(c); err == nil {
			out = append(out, obj)
		}
	}
	return out
}

func rawActions(in []alerting.ActionSpec) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, a := range in {
		if obj, err := toObject(a); err == nil {
			out = append(out, obj)
		}
	}
	return out
}

// toObject and fromObject round-trip through JSON rather than reflecting.
//
// It is the encoding that defines these shapes — the column is jsonb and the
// domain decodes it strictly — so going through the encoder is the only mapping
// that cannot disagree with the one the store performs.
func toObject(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(raw, &out)
}

func fromObject(obj map[string]any, into any) error {
	raw, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// toWatch maps a request body onto the domain type. The conditions and actions
// are decoded strictly by the domain's own builder, so a malformed one is refused
// there rather than half-decoded here.
func toWatch(b watchBody) (alerting.Watch, error) {
	w := alerting.Watch{
		ID: b.ID, Name: b.Name, Description: b.Description, Enabled: b.Enabled,
		Severity: b.Severity, Combinator: alerting.Combinator(b.Combinator),
		OnNoData: alerting.NoDataPolicy(b.OnNoData),
		Step:     time.Duration(b.StepSeconds) * time.Second,
		Interval: time.Duration(b.IntervalSeconds) * time.Second,
		For:      time.Duration(b.ForSeconds) * time.Second,
		Renotify: time.Duration(b.RenotifySeconds) * time.Second,
	}
	for _, obj := range b.Conditions {
		var spec alerting.ConditionSpec
		if err := fromObject(obj, &spec); err != nil {
			return alerting.Watch{}, err
		}
		w.Conditions = append(w.Conditions, spec)
	}
	for _, obj := range b.Actions {
		var spec alerting.ActionSpec
		if err := fromObject(obj, &spec); err != nil {
			return alerting.Watch{}, err
		}
		w.Actions = append(w.Actions, spec)
	}
	return w, nil
}

func toStateBody(s alerting.State) watchStateBody {
	return watchStateBody{
		Phase: string(s.Phase), Since: nonZero(s.Since),
		ConsecutiveFiring: s.ConsecutiveFiring, ConsecutiveOK: s.ConsecutiveOK,
		LastEvalAt: nonZero(s.LastEvalAt), LastStatus: string(s.LastStatus),
		LastValue: s.LastValue, IncidentID: s.IncidentID,
		MutedUntil: nonZero(s.MutedUntil), NextDueAt: nonZero(s.NextDueAt),
	}
}

func toIncidentBody(i alerting.Incident) incidentBody {
	return incidentBody{
		ID: i.ID, WatchID: i.WatchID, WatchName: i.WatchName,
		OpenedAt: i.OpenedAt, ResolvedAt: i.ResolvedAt, ClosedReason: i.ClosedReason,
		Severity: i.Severity, AcknowledgedAt: i.AcknowledgedAt,
		OpenedMatched: i.OpenedMatched, OpenedTotal: i.OpenedTotal,
		OpenedOutcomes: nonNilOutcomes(i.OpenedOutcomes),
		Evaluations:    i.Evaluations, Notifications: i.Notifications,
	}
}

func toEvaluationBody(e alerting.EvaluationRecord) evaluationBody {
	return evaluationBody{
		ID: e.ID, WatchID: e.WatchID, IncidentID: e.IncidentID, EvaluatedAt: e.EvaluatedAt,
		Status: string(e.Status), Phase: string(e.Phase), PreviousPhase: string(e.PreviousPhase),
		Transitioned: e.Transitioned, Degraded: e.Degraded, Matched: e.Matched, Total: e.Total,
		WindowFrom: e.WindowFrom, WindowTo: e.WindowTo,
		Reason: e.Reason, Error: e.Err, DurationMs: e.DurationMS,
		Outcomes: nonNilOutcomes(e.Outcomes),
	}
}

// nonNilOutcomes renders an empty list as [] rather than null, so a client can
// iterate without a nil check on every row.
func nonNilOutcomes(in []alerting.Outcome) []alerting.Outcome {
	if in == nil {
		return []alerting.Outcome{}
	}
	return in
}

func nonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
