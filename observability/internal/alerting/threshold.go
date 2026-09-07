package alerting

import (
	"fmt"
	"time"
)

const (
	// defaultThresholdWindow is how many buckets one comparison covers when the
	// condition names none. Five one-minute buckets is the shortest window in
	// which a rate means anything.
	defaultThresholdWindow = 5

	// defaultMinDenominator is the smallest number of trials a ratio may be
	// judged from at all. The Wilson bound already scales with the evidence, so
	// this is a floor under the floor rather than the main defence — it stops a
	// watch reporting a confident-looking rate over three requests.
	defaultMinDenominator = 20
)

// ThresholdParams is "is this number above (or below) that one".
type ThresholdParams struct {
	Op        Op      `json:"op"`
	Threshold float64 `json:"threshold"`

	// WindowBuckets is the width of the comparison, in the watch's buckets. It is
	// also the whole of the smoothing this package does: summing five one-minute
	// buckets is a five-minute rate with no phase lag and no invented points, and
	// an EWMA on top would delay every alert by its own time constant to buy a
	// noise reduction the window already bought.
	WindowBuckets int `json:"windowBuckets,omitempty"`
	MinSamples    int `json:"minSamples,omitempty"`

	// MinDenominator and Confidence apply to ratio metrics only.
	Confidence     float64 `json:"confidence,omitempty"`
	MinDenominator float64 `json:"minDenominator,omitempty"`
}

type thresholdCondition struct {
	conditionBase
	params ThresholdParams
}

func newThreshold(base conditionBase) (Condition, error) {
	var p ThresholdParams
	if err := decodeParams(base.spec.Params, &p); err != nil {
		return nil, err
	}
	switch p.Op {
	case OpGT, OpGTE, OpLT, OpLTE:
	default:
		return nil, fmt.Errorf("alerting: %w: threshold needs one of gt/gte/lt/lte, got %q",
			ErrInvalidParams, p.Op)
	}
	if p.WindowBuckets <= 0 {
		p.WindowBuckets = defaultThresholdWindow
	}
	if p.MinSamples <= 0 {
		// Half a window, rounded up. A window with a hole in it is still a
		// window; a window that is mostly hole is not.
		p.MinSamples = (p.WindowBuckets + 1) / 2
	}
	if p.MinSamples > p.WindowBuckets {
		return nil, fmt.Errorf("alerting: %w: minSamples %d exceeds the %d-bucket window",
			ErrInvalidParams, p.MinSamples, p.WindowBuckets)
	}
	if base.metric.Ratio() {
		if p.Confidence <= 0 {
			p.Confidence = defaultConfidence
		}
		if p.MinDenominator <= 0 {
			p.MinDenominator = defaultMinDenominator
		}
	}
	return &thresholdCondition{conditionBase: base, params: p}, nil
}

func (c *thresholdCondition) Kind() string   { return KindThreshold }
func (c *thresholdCondition) Downward() bool { return c.params.Op.Downward() }

func (c *thresholdCondition) Label() string {
	return fmt.Sprintf("%s %s over %s%s",
		c.metric.Name, c.params.Op, windowLabel(c.params.WindowBuckets, c.step), scopeLabel(c.spec.Scope))
}

func (c *thresholdCondition) Query(now time.Time) Query {
	return c.query(now, c.params.WindowBuckets)
}

func (c *thresholdCondition) Unavailable(reason, err string) Outcome {
	out := c.outcome(KindThreshold, c.Label(), c.params.Op, c.params.Threshold)
	out.Err = err
	return out.finish(Unknown, reason)
}

func (c *thresholdCondition) Evaluate(now time.Time, s Series) Outcome {
	out := c.outcome(KindThreshold, c.Label(), c.params.Op, c.params.Threshold)
	from, to := windowIndices(s, now, c.params.WindowBuckets)
	out.WindowFrom, out.WindowTo = s.BucketStart(from), s.BucketStart(to)

	if c.metric.Ratio() {
		return c.evaluateRatio(s, from, to, out)
	}

	known := s.Known(from, to)
	out.Samples = len(known)
	if len(known) == 0 {
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}
	if len(known) < c.params.MinSamples {
		return out.finish(Unknown, ReasonFewSamples)
	}
	observed, err := Reduce(c.agg, known)
	if err != nil {
		out.Err = err.Error()
		return out.finish(Unknown, ReasonFetchFailed)
	}
	out.Observed = &observed
	if c.params.Op.Compare(observed, c.params.Threshold) {
		return out.finish(True, ReasonConditionMet)
	}
	return out.finish(False, ReasonThresholdUnmet)
}

// evaluateRatio judges a proportion by its confidence bound rather than by its
// point estimate.
//
// This is the single most load-bearing comparison in the package. One failure out
// of two traces is a 50% error rate by the point estimate, and a 10% threshold
// would page somebody about two requests; the bound at two trials is about 9% and
// does not fire, while at four hundred trials it is about 45% and does. Observed
// still reports the point estimate, because that is the number a human
// recognises — Score carries what the comparison was actually made against.
func (c *thresholdCondition) evaluateRatio(s Series, from, to int, out Outcome) Outcome {
	numerators := s.Known(from, to)
	out.Samples = len(numerators)
	if len(numerators) == 0 {
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}
	if len(numerators) < c.params.MinSamples {
		// The same gate the non-ratio path applies, and it is not redundant with
		// the denominator floor below: a window can carry plenty of trials in one
		// surviving bucket while the rest of it never reported, which is a rate
		// measured over a moment rather than over the window somebody asked for.
		return out.finish(Unknown, ReasonFewSamples)
	}
	var numerator float64
	for _, v := range numerators {
		numerator += v
	}
	denominator := s.SumDenominator(from, to)
	out.Denominator = denominator
	if denominator == 0 {
		// Undefined, not zero. A window in which nothing ran has no error rate,
		// and reporting one as 0% would satisfy every downward condition here.
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}

	point := numerator / denominator
	out.Observed = &point
	if denominator < c.params.MinDenominator {
		return out.finish(Unknown, ReasonSmallDenom)
	}
	bound, ok := Bound(c.params.Op, numerator, denominator, c.params.Confidence)
	if !ok {
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}
	out.Score = &bound
	if c.params.Op.Compare(bound, c.params.Threshold) {
		return out.finish(True, ReasonConditionMet)
	}
	return out.finish(False, ReasonThresholdUnmet)
}
