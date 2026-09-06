package alerting

import (
	"fmt"
	"time"
)

const (
	// defaultAbsenceFor is how many consecutive empty buckets count as silence.
	defaultAbsenceFor = 5
	// defaultAbsenceBaseline is how much history must show the series reporting
	// before its silence means anything.
	defaultAbsenceBaseline = 12
)

// AbsenceParams is "this used to report and has stopped".
//
// A separate kind rather than a threshold of "count below one", because the two
// facts it needs are ones a comparison cannot express. Zero and unknown are
// different states and the operators do not distinguish them; and the
// precondition — that the series reported at all before it went quiet — has no
// home in a threshold. Without that precondition a watch on a deployment that
// never existed fires forever, which is the classic way an absence alert becomes
// noise nobody reads.
type AbsenceParams struct {
	ForBuckets  int `json:"forBuckets,omitempty"`
	MinBaseline int `json:"minBaseline,omitempty"`

	// BaselineBuckets is how far back the "it used to report" evidence is looked
	// for, beyond the silent stretch itself.
	BaselineBuckets int `json:"baselineBuckets,omitempty"`
}

type absenceCondition struct {
	conditionBase
	params AbsenceParams
}

func newAbsence(base conditionBase) (Condition, error) {
	var p AbsenceParams
	if err := decodeParams(base.spec.Params, &p); err != nil {
		return nil, err
	}
	if p.ForBuckets <= 0 {
		p.ForBuckets = defaultAbsenceFor
	}
	if p.MinBaseline <= 0 {
		p.MinBaseline = defaultAbsenceBaseline
	}
	if p.BaselineBuckets <= 0 {
		p.BaselineBuckets = defaultBaseline
	}
	if p.MinBaseline > p.BaselineBuckets {
		return nil, fmt.Errorf("alerting: %w: minBaseline %d exceeds the %d-bucket baseline",
			ErrInvalidParams, p.MinBaseline, p.BaselineBuckets)
	}
	return &absenceCondition{conditionBase: base, params: p}, nil
}

func (c *absenceCondition) Kind() string { return KindAbsence }

// Downward is unconditionally true: silence is what a dead ingest pipeline looks
// like, so this is exactly the kind the runner suppresses while nothing at all is
// arriving.
func (c *absenceCondition) Downward() bool { return true }

func (c *absenceCondition) Label() string {
	return fmt.Sprintf("%s silent for %s%s",
		c.metric.Name, windowLabel(c.params.ForBuckets, c.step), scopeLabel(c.spec.Scope))
}

func (c *absenceCondition) Query(now time.Time) Query {
	return c.query(now, c.params.ForBuckets+c.params.BaselineBuckets)
}

func (c *absenceCondition) Unavailable(reason, err string) Outcome {
	out := c.outcome(KindAbsence, c.Label(), OpLTE, 0)
	out.Err = err
	return out.finish(Unknown, reason)
}

func (c *absenceCondition) Evaluate(now time.Time, s Series) Outcome {
	out := c.outcome(KindAbsence, c.Label(), OpLTE, 0)
	from, to := windowIndices(s, now, c.params.ForBuckets)
	out.WindowFrom, out.WindowTo = s.BucketStart(from), s.BucketStart(to)
	if to <= from {
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}

	// Reporting nothing and reporting a zero both count as silent here, which is
	// the one place in this package where the two are deliberately the same. The
	// question is whether anything happened, and neither a missing bucket nor an
	// empty one says it did.
	silent := 0.0
	out.Observed = &silent
	out.Samples = to - from
	for i := from; i < to; i++ {
		if v, ok := s.At(i); ok && v != 0 {
			return out.finish(False, ReasonThresholdUnmet)
		}
	}

	reported := 0
	for i := maxInt(from-c.params.BaselineBuckets, 0); i < from; i++ {
		if v, ok := s.At(i); ok && v != 0 {
			reported++
		}
	}
	out.BaselineSamples = reported
	if reported < c.params.MinBaseline {
		// It has always been quiet. That is not an outage, and firing on it is
		// how a watch on a deployment that never ran pages somebody forever.
		return out.finish(False, ReasonNeverReported)
	}
	return out.finish(True, ReasonConditionMet)
}
