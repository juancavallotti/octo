package alerting

import "time"

// Reason codes. A condition that declined to fire says which gate stopped it,
// because "why did this not go off when I thought it would" is the question asked
// after every missed incident, and it is unanswerable from a bare false.
const (
	ReasonNoData         = "no_data"
	ReasonFewSamples     = "below_min_samples"
	ReasonShortBaseline  = "below_min_baseline"
	ReasonSmallDenom     = "denominator_too_small"
	ReasonSmallDelta     = "below_min_delta"
	ReasonSmallRatio     = "below_min_ratio"
	ReasonBelowZ         = "below_z"
	ReasonNeverReported  = "never_reported"
	ReasonNoIngest       = "no_ingest"
	ReasonFetchFailed    = "fetch_failed"
	ReasonWatchInvalid   = "watch_invalid"
	ReasonThresholdUnmet = "threshold_unmet"
	ReasonConditionMet   = "condition_met"
)

// Outcome is one condition's answer, and it is deliberately self-contained.
//
// It carries the threshold it was judged against and the label it was judged
// under as they were at the moment it ran, so a history row from three weeks ago
// still explains itself after the watch has been retuned. A row that stored only
// the observed value and looked the threshold up on read would render a sentence
// that was never true.
type Outcome struct {
	ConditionID string  `json:"conditionId"`
	Kind        string  `json:"kind"`
	Label       string  `json:"label"`
	Unit        Unit    `json:"unit,omitempty"`
	Op          Op      `json:"op,omitempty"`
	Threshold   float64 `json:"threshold"`

	// Observed is the statistic in the units a human recognises — for a ratio
	// that is the point estimate, not the confidence bound the comparison was
	// actually made against. Score carries the bound, or the robust z.
	Observed *float64 `json:"observed"`
	Baseline *float64 `json:"baseline,omitempty"`
	Score    *float64 `json:"score,omitempty"`

	Samples         int     `json:"samples"`
	BaselineSamples int     `json:"baselineSamples,omitempty"`
	Denominator     float64 `json:"denominator,omitempty"`

	WindowFrom time.Time `json:"windowFrom"`
	WindowTo   time.Time `json:"windowTo"`

	Verdict Truth  `json:"-"`
	Truth   string `json:"verdict"`
	NoData  bool   `json:"noData,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Err     string `json:"error,omitempty"`
}

// finish stamps the wire form of the verdict. Truth is an integer with an
// iota-derived zero, and serializing that as a number would make False and the
// absence of a verdict identical in stored JSON.
func (o Outcome) finish(v Truth, reason string) Outcome {
	o.Verdict = v
	o.Truth = v.String()
	if reason != "" {
		o.Reason = reason
	}
	return o
}

// Status is what one evaluation of a whole watch decided.
type Status string

const (
	StatusOK           Status = "ok"           // evaluated, the watch does not hold
	StatusFiring       Status = "firing"       // evaluated, the watch holds
	StatusInsufficient Status = "insufficient" // not enough data to decide
	StatusSkipped      Status = "skipped"      // deliberately not evaluated
	StatusError        Status = "error"        // nothing could be evaluated at all
)

// Evaluation is one tick of one watch: every condition's answer, and the verdict
// they combine to.
//
// Matched and Total are the composite verdict in the form the UI says out loud —
// "fired because 2 of 3 matched" — and are lifted onto columns in the history
// table so the list filters and sorts without decoding the per-condition detail.
type Evaluation struct {
	At         time.Time  `json:"at"`
	Combinator Combinator `json:"combinator"`
	Outcomes   []Outcome  `json:"outcomes"`

	Verdict Truth  `json:"-"`
	Status  Status `json:"status"`
	Matched int    `json:"matched"`
	Total   int    `json:"total"`

	// Degraded marks an evaluation in which at least one condition could not be
	// answered. It is not the same as an error: under `any` a sibling that is
	// genuinely true still fires, and the row should record that it did so while
	// blind in one eye.
	Degraded bool `json:"degraded"`

	WindowFrom time.Time     `json:"windowFrom"`
	WindowTo   time.Time     `json:"windowTo"`
	Duration   time.Duration `json:"-"`
	Reason     string        `json:"reason,omitempty"`
	Err        string        `json:"error,omitempty"`
}

// Firing reports whether this evaluation says the watch holds.
func (e Evaluation) Firing() bool { return e.Verdict == True }

// Decided reports whether the evaluation reached a verdict at all. An undecided
// evaluation moves no state: it neither advances a hold nor resolves an incident,
// because an incident closed by the absence of evidence is an outage recorded as
// fixed.
func (e Evaluation) Decided() bool { return e.Verdict != Unknown }
