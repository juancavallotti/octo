package alerting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// The condition kinds. Three, over a metric layer that already knows how to be a
// ratio: the metric says what number to compute per bucket and the kind says how
// to judge it. That split is what stops the vocabulary growing a kind per metric —
// "error rate above 5%" and "error rate suddenly doubled" are one number judged
// two ways, not two condition types.
const (
	KindThreshold = "threshold"
	KindSpike     = "spike"
	KindAbsence   = "absence"

	// KindGroup is accepted by the decoder and refused by the builder. The stored
	// array is already a tree of depth one, so nesting can arrive later without
	// moving a row; until it does, a group must fail loudly rather than evaluate
	// as an empty condition.
	KindGroup = "group"
)

// MaxConditions bounds one watch. It caps the fan-out of a single evaluation, and
// no honest alert needs more than this many clauses.
const MaxConditions = 10

// ConditionSpec is a condition as it is stored: the common fields every kind has,
// plus that kind's own parameters as raw JSON.
//
// The parameters are raw here and decoded strictly by the builder. A misspelled
// parameter that decoded permissively would leave a threshold at its zero value —
// which for most of them means a condition that fires on everything or on
// nothing, silently, forever.
type ConditionSpec struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Source    Source    `json:"source"`
	Metric    string    `json:"metric"`
	Aggregate Aggregate `json:"aggregate,omitempty"`
	Scope     Scope     `json:"scope,omitempty"`

	Params json.RawMessage `json:"params,omitempty"`

	// Conditions carries a nested group's children. Present in the type so the
	// stored shape does not have to change when nesting lands; refused by the
	// builder until it does.
	Conditions []ConditionSpec `json:"conditions,omitempty"`
}

// Condition is a built, validated condition: it knows what to fetch and how to
// judge what comes back.
//
// An interface here rather than a switch because there are genuinely three
// implementations with three sets of parameters and three failure modes, and the
// alternative is one Evaluate function with a switch at the top and every kind's
// gates interleaved in one body.
type Condition interface {
	ID() string
	Kind() string
	Label() string
	// Query is the whole span this condition needs — window, guard band and
	// baseline in one fetch.
	Query(now time.Time) Query
	// Evaluate judges a series that may be WIDER than the query asked for,
	// because queries are coalesced. Implementations locate their window by time
	// rather than by offset from the end.
	Evaluate(now time.Time, s Series) Outcome
	// Unavailable is the outcome when the fetch failed, so the row still carries
	// the label and threshold that would have been judged.
	Unavailable(reason, err string) Outcome
	// Downward reports whether this condition fires on small numbers, and so
	// whether a dead ingest pipeline would satisfy it for the wrong reason.
	Downward() bool
}

// NewCondition builds a condition from its stored form.
//
// This is also the validator: a watch is saved through it, so a definition that
// cannot produce a working condition is refused while somebody is looking at the
// form rather than at three in the morning.
//
// An unrecognised type is an error, never a skip. Dropping a condition makes an
// `all` watch fire more readily and an `any` watch fire less, and both are wrong
// in a way nobody would notice — which is the one bug in this design that could
// ship silently and page nobody.
func NewCondition(spec ConditionSpec, step time.Duration) (Condition, error) {
	if spec.ID == "" {
		return nil, fmt.Errorf("alerting: %w: a condition needs an id", ErrInvalidParams)
	}
	if spec.Type == KindGroup || len(spec.Conditions) > 0 {
		return nil, fmt.Errorf("alerting: %w: condition %q", ErrNestedConditions, spec.ID)
	}

	metric, err := LookupMetric(spec.Source, spec.Metric)
	if err != nil {
		return nil, err
	}
	agg := spec.Aggregate
	if agg == "" {
		agg = metric.DefaultAggregate()
	}
	if !metric.Allows(agg) {
		return nil, fmt.Errorf("alerting: %w: %s does not take %s",
			ErrInvalidParams, metric.Name, agg)
	}
	base := conditionBase{spec: spec, metric: metric, agg: agg, step: step}

	switch spec.Type {
	case KindThreshold:
		return newThreshold(base)
	case KindSpike:
		return newSpike(base)
	case KindAbsence:
		return newAbsence(base)
	default:
		return nil, fmt.Errorf("alerting: %w: %q on condition %q", ErrUnknownCondition, spec.Type, spec.ID)
	}
}

// conditionBase is what all three kinds share: the stored spec, the resolved
// metric, the aggregate in force and the bucket width from the watch.
type conditionBase struct {
	spec   ConditionSpec
	metric Metric
	agg    Aggregate
	step   time.Duration
}

func (c conditionBase) ID() string { return c.spec.ID }

// span is how far back this condition needs data, in buckets. Each kind
// overrides it; the base is the safe minimum.
func (c conditionBase) query(now time.Time, buckets int) Query {
	from, to := Window(now, c.step, buckets)
	return Query{
		Source:    c.spec.Source,
		Metric:    c.metric.Name,
		Aggregate: c.agg,
		Scope:     c.spec.Scope,
		Step:      c.step,
		From:      from,
		To:        to,
	}
}

// outcome pre-fills the fields every result carries, so a fetch failure and a
// clean evaluation describe themselves the same way.
func (c conditionBase) outcome(kind, label string, op Op, threshold float64) Outcome {
	return Outcome{
		ConditionID: c.spec.ID,
		Kind:        kind,
		Label:       label,
		Unit:        c.metric.Unit,
		Op:          op,
		Threshold:   threshold,
	}
}

// windowIndices locates the evaluation window inside a series that may be wider
// than this condition asked for.
//
// It is computed from `now` rather than from the end of the series precisely
// because of coalescing: a sibling condition with a longer baseline widens the
// fetch, and a condition that took "the last W buckets" would silently evaluate
// the sibling's window instead of its own.
func windowIndices(s Series, now time.Time, buckets int) (from, to int) {
	if s.Step <= 0 || s.Len() == 0 {
		return 0, 0
	}
	end := AlignDown(now.Add(-EvalLag), s.Step)
	to = int((end.UnixMilli() - s.StartMS) / s.Step.Milliseconds())
	if to > s.Len() {
		to = s.Len()
	}
	if to < 0 {
		to = 0
	}
	from = to - buckets
	if from < 0 {
		from = 0
	}
	return from, to
}

// decodeParams unmarshals a kind's parameters strictly. A field the kind does not
// know is an error, because the alternative is a threshold left at zero by a typo.
func decodeParams(raw json.RawMessage, into any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("alerting: %w: %w", ErrInvalidParams, err)
	}
	return nil
}

// windowLabel renders a bucket count as the duration a human wrote.
func windowLabel(buckets int, step time.Duration) string {
	return (time.Duration(buckets) * step).String()
}

// scopeLabel names what a condition was narrowed to, for the sentence an incident
// page renders. Empty when the watch covers the whole installation.
func scopeLabel(s Scope) string {
	switch {
	case s.AppName != "":
		return ", app " + s.AppName
	case s.IntegrationID != "":
		return ", integration " + s.IntegrationID
	case s.DeploymentID != "":
		return ", deployment " + s.DeploymentID
	default:
		return ""
	}
}
