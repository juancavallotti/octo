package alerting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// MinInterval is how often a watch may be asked. Below a minute the
	// evaluation lag is a large fraction of the gap between ticks, so a watch
	// would mostly be re-reading buckets it has already judged.
	MinInterval = time.Minute
	MaxInterval = time.Hour

	// MaxName bounds a watch name, which is rendered into notification subjects.
	MaxName = 200
)

// ActionSpec is what to do when a watch fires, stored on the same terms as a
// condition: a stable id, a type discriminator, and that type's own payload.
// Dispatch lives with the notifier; this is only the shape the definition carries.
type ActionSpec struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Watch is a standing question, exactly as it is stored.
//
// One schedule for the whole set: Interval is how often the conditions are asked
// together, For is how long their combined verdict must hold, and Renotify is how
// often a still-firing watch says so again. Nothing here is per condition, which
// is the point — a watch is the unit a human reasons about and the unit an action
// fires for.
type Watch struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
	Severity    string

	Combinator Combinator
	Conditions []ConditionSpec
	Actions    []ActionSpec
	OnNoData   NoDataPolicy

	Step     time.Duration
	Interval time.Duration
	For      time.Duration
	Renotify time.Duration

	DefinitionHash string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HoldEvaluations is how many consecutive firing evaluations For amounts to.
//
// Counted in evaluations rather than compared as wall-clock time, because five
// minutes of firing with an evaluation missing in the middle is not five minutes
// of firing — and a wall-clock comparison would call it one, which is the whole
// failure the hold exists to prevent. It follows that changing the interval
// changes how many samples the hold demands, and that is the intended reading.
func (w Watch) HoldEvaluations() int {
	if w.For <= 0 || w.Interval <= 0 {
		return 1
	}
	n := int((w.For + w.Interval - 1) / w.Interval)
	if n < 1 {
		return 1
	}
	return n
}

// Built is a validated watch with its conditions constructed.
//
// Building is the validation: a definition that cannot produce working conditions
// is refused while somebody is looking at the form. It is also where a definition
// stored by a newer version of this service is caught, and the catch is loud —
// the watch is marked invalid and skipped rather than evaluated with a condition
// silently missing.
type Built struct {
	Watch      Watch
	Conditions []Condition
}

// Build validates a watch and constructs its conditions.
func Build(w Watch) (*Built, error) {
	if err := validate(w); err != nil {
		return nil, err
	}
	built := make([]Condition, 0, len(w.Conditions))
	seen := make(map[string]struct{}, len(w.Conditions))
	for _, spec := range w.Conditions {
		if _, dup := seen[spec.ID]; dup {
			return nil, fmt.Errorf("alerting: %w: two conditions share the id %q", ErrInvalidWatch, spec.ID)
		}
		seen[spec.ID] = struct{}{}
		c, err := NewCondition(spec, w.Step)
		if err != nil {
			return nil, err
		}
		built = append(built, c)
	}
	return &Built{Watch: w, Conditions: built}, nil
}

func validate(w Watch) error {
	switch {
	case strings.TrimSpace(w.Name) == "":
		return fmt.Errorf("alerting: %w: a watch needs a name", ErrInvalidWatch)
	case len(w.Name) > MaxName:
		return fmt.Errorf("alerting: %w: a name may not exceed %d characters", ErrInvalidWatch, MaxName)
	case w.Combinator != CombineAll && w.Combinator != CombineAny:
		return fmt.Errorf("alerting: %w: combinator must be all or any, got %q", ErrInvalidWatch, w.Combinator)
	case len(w.Conditions) == 0:
		return fmt.Errorf("alerting: %w: a watch with no conditions asks nothing", ErrInvalidWatch)
	case len(w.Conditions) > MaxConditions:
		return fmt.Errorf("alerting: %w: %d conditions exceeds the limit of %d",
			ErrInvalidWatch, len(w.Conditions), MaxConditions)
	case w.Step < MinStep || w.Step > MaxStep:
		return fmt.Errorf("alerting: %w: step must be between %s and %s, got %s",
			ErrInvalidWatch, MinStep, MaxStep, w.Step)
	case w.Interval < MinInterval || w.Interval > MaxInterval:
		return fmt.Errorf("alerting: %w: interval must be between %s and %s, got %s",
			ErrInvalidWatch, MinInterval, MaxInterval, w.Interval)
	case w.For < 0 || w.Renotify < 0:
		return fmt.Errorf("alerting: %w: for and renotify may not be negative", ErrInvalidWatch)
	case !ValidSeverity(w.Severity):
		return fmt.Errorf("alerting: %w: severity must be info, warning or critical, got %q",
			ErrInvalidWatch, w.Severity)
	}
	return nil
}

// Downward reports whether any condition fires on small numbers. The runner asks
// this before evaluating, because those are the conditions a dead ingest pipeline
// would satisfy for entirely the wrong reason.
func (b *Built) Downward() bool {
	for _, c := range b.Conditions {
		if c.Downward() {
			return true
		}
	}
	return false
}

// Plan is the set of fetches this watch needs, coalesced.
//
// Conditions that read the same rows at the same resolution share one query
// covering the union of their spans, so a watch with three clauses on trace error
// rate does one index scan rather than three. The union is always contiguous
// because both bounds are aligned to a step the whole watch shares.
//
// The caller must derive every span from ONE `now`. That is not hygiene: two
// conditions each reading the clock produce spans differing by microseconds,
// which never coalesce, and the watch still returns correct answers at three
// times the cost — the worst kind of bug, because nothing fails.
func (b *Built) Plan(now time.Time) []Query {
	order := make([]string, 0, len(b.Conditions))
	byKey := make(map[string]Query, len(b.Conditions))
	for _, c := range b.Conditions {
		q := c.Query(now)
		key := q.Key()
		if existing, ok := byKey[key]; ok {
			byKey[key] = existing.Widen(q)
			continue
		}
		byKey[key] = q
		order = append(order, key)
	}
	out := make([]Query, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

// Fetched is one query's result: the series, or why there is none.
type Fetched struct {
	Series Series
	Err    error
}

// Evaluate judges every condition against what came back and combines the
// verdicts.
//
// Conditions are heterogeneous by design — "error rate up AND memory above X" is
// the alert people cannot express in a single-source tool, and it is most of the
// reason this is worth building. Nothing here special-cases a source: a query
// carries its own, failures are per query and therefore per backend, and the
// three-valued combine then does the right thing without a rule about Redis
// having to be written anywhere.
func (b *Built) Evaluate(now time.Time, fetched map[string]Fetched) Evaluation {
	eval := Evaluation{
		At:         now,
		Combinator: b.Watch.Combinator,
		Outcomes:   make([]Outcome, 0, len(b.Conditions)),
		Total:      len(b.Conditions),
	}
	verdicts := make([]Truth, 0, len(b.Conditions))

	for _, c := range b.Conditions {
		out := b.evaluateOne(c, now, fetched)
		if out.Err != "" || out.Verdict == Unknown {
			eval.Degraded = true
		}
		if out.Verdict == True {
			eval.Matched++
		}
		eval.Outcomes = append(eval.Outcomes, out)
		verdicts = append(verdicts, out.Verdict)
		eval.WindowFrom, eval.WindowTo = widenSpan(eval.WindowFrom, eval.WindowTo, out.WindowFrom, out.WindowTo)
	}

	eval.Verdict = Combine(b.Watch.Combinator, verdicts)
	eval.Status = statusOf(eval)
	return eval
}

// evaluateOne runs a single condition and applies the watch's no-data policy.
//
// The policy is applied here rather than inside the condition because it belongs
// to the watch: two conditions in one set must read an empty window the same way,
// or `all` means something different depending on which clause ran out of data.
func (b *Built) evaluateOne(c Condition, now time.Time, fetched map[string]Fetched) Outcome {
	result, ok := fetched[c.Query(now).Key()]
	switch {
	case !ok:
		return c.Unavailable(ReasonFetchFailed, "no result was fetched for this condition")
	case result.Err != nil:
		return c.Unavailable(ReasonFetchFailed, result.Err.Error())
	}
	out := c.Evaluate(now, result.Series)
	if out.NoData {
		return out.finish(b.Watch.OnNoData.Truth(), out.Reason)
	}
	return out
}

// statusOf maps a combined verdict onto the status a history row carries. An
// evaluation in which every condition failed to fetch is an error rather than
// merely undecided: the difference is whether anything was wrong with this
// service, and an operator reading the history needs to see that.
func statusOf(e Evaluation) Status {
	switch e.Verdict {
	case True:
		return StatusFiring
	case False:
		return StatusOK
	default:
		if allErrored(e.Outcomes) {
			return StatusError
		}
		return StatusInsufficient
	}
}

func allErrored(outcomes []Outcome) bool {
	if len(outcomes) == 0 {
		return false
	}
	for _, o := range outcomes {
		if o.Err == "" {
			return false
		}
	}
	return true
}

func widenSpan(from, to, candidateFrom, candidateTo time.Time) (time.Time, time.Time) {
	if candidateFrom.IsZero() && candidateTo.IsZero() {
		return from, to
	}
	if from.IsZero() || candidateFrom.Before(from) {
		from = candidateFrom
	}
	if candidateTo.After(to) {
		to = candidateTo
	}
	return from, to
}

// hashable is the part of a definition that changes what a pending clock means.
//
// Deliberately not the whole watch. Renaming a watch or changing who it emails
// must not restart a hold that is three evaluations into a five-evaluation wait;
// changing a threshold must, because the clock was measuring a different question
// and carrying it across the edit would fire an alert on evidence gathered for a
// condition that no longer exists.
type hashable struct {
	Combinator Combinator      `json:"combinator"`
	Conditions []ConditionSpec `json:"conditions"`
	OnNoData   NoDataPolicy    `json:"onNoData"`
	StepMS     int64           `json:"stepMs"`
	ForMS      int64           `json:"forMs"`
	IntervalMS int64           `json:"intervalMs"`
}

// Fingerprint computes the meaning-bearing hash of this definition, which the
// store writes into DefinitionHash.
func (w Watch) Fingerprint() (string, error) {
	raw, err := json.Marshal(hashable{
		Combinator: w.Combinator,
		Conditions: w.Conditions,
		OnNoData:   w.OnNoData,
		StepMS:     w.Step.Milliseconds(),
		ForMS:      w.For.Milliseconds(),
		IntervalMS: w.Interval.Milliseconds(),
	})
	if err != nil {
		return "", fmt.Errorf("alerting: fingerprint a watch definition: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
