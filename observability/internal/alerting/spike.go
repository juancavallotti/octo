package alerting

import (
	"fmt"

	"time"
)

const (
	// The spike defaults. Every one of them is a gate that has to be cleared, and
	// between them they are the answer to "why did my quiet integration not page
	// me when it went from one request to three".
	defaultSpikeWindow   = 1
	defaultGuardBuckets  = 1
	defaultBaseline      = 30
	defaultZ             = 4
	defaultMinRatio      = 2
	defaultMinBaseline   = 12
	defaultCountMinDelta = 5

	// DirectionUp and DirectionDown are the two things a spike can be.
	DirectionUp   = "up"
	DirectionDown = "down"
)

// SpikeParams is "has this number suddenly changed", judged against the series'
// own recent history.
type SpikeParams struct {
	// WindowBuckets is how much recent data counts as "now", and BaselineBuckets
	// how much history it is compared against.
	WindowBuckets   int `json:"windowBuckets,omitempty"`
	BaselineBuckets int `json:"baselineBuckets,omitempty"`

	// GuardBuckets is the gap held open between the two. It is not decoration: a
	// ramp that has been climbing for three buckets would otherwise supply its
	// own baseline, and a condition whose baseline follows it up never fires.
	GuardBuckets int `json:"guardBuckets,omitempty"`

	Direction string `json:"direction,omitempty"`

	// The three gates, all of which must clear.
	//
	// Z is the robust z-score, which asks whether the change is large relative to
	// how much this series usually moves. MinDelta asks whether it is large in
	// absolute terms, and MinRatio whether it is large in proportion. The
	// statistical gate alone is not enough and that is the point: over a quiet
	// enough history a robust z will eventually call any change significant,
	// and MinDelta is what says "I do not care about two more requests however
	// surprising they are".
	Z        float64 `json:"z,omitempty"`
	MinDelta float64 `json:"minDelta,omitempty"`
	MinRatio float64 `json:"minRatio,omitempty"`

	MinSamples     int     `json:"minSamples,omitempty"`
	MinBaseline    int     `json:"minBaseline,omitempty"`
	MinScale       float64 `json:"minScale,omitempty"`
	MinDenominator float64 `json:"minDenominator,omitempty"`
}

type spikeCondition struct {
	conditionBase
	params SpikeParams
}

func newSpike(base conditionBase) (Condition, error) {
	var p SpikeParams
	if err := decodeParams(base.spec.Params, &p); err != nil {
		return nil, err
	}
	if p.Direction == "" {
		p.Direction = DirectionUp
	}
	if p.Direction != DirectionUp && p.Direction != DirectionDown {
		return nil, fmt.Errorf("alerting: %w: spike direction must be up or down, got %q",
			ErrInvalidParams, p.Direction)
	}
	applySpikeDefaults(&p, base.metric)
	if p.MinSamples > p.WindowBuckets {
		return nil, fmt.Errorf("alerting: %w: minSamples %d exceeds the %d-bucket window",
			ErrInvalidParams, p.MinSamples, p.WindowBuckets)
	}
	return &spikeCondition{conditionBase: base, params: p}, nil
}

func applySpikeDefaults(p *SpikeParams, metric Metric) {
	if p.WindowBuckets <= 0 {
		p.WindowBuckets = defaultSpikeWindow
	}
	if p.BaselineBuckets <= 0 {
		p.BaselineBuckets = defaultBaseline
	}
	if p.GuardBuckets < 0 {
		p.GuardBuckets = 0
	} else if p.GuardBuckets == 0 {
		p.GuardBuckets = defaultGuardBuckets
	}
	if p.Z <= 0 {
		p.Z = defaultZ
	}
	if p.MinRatio <= 0 {
		p.MinRatio = defaultMinRatio
	}
	if p.MinSamples <= 0 {
		p.MinSamples = 1
	}
	if p.MinBaseline <= 0 {
		p.MinBaseline = defaultMinBaseline
	}
	// A counting metric gets an absolute floor by default, because that is the
	// gate that makes the quiet-series guarantee. Anything else — money, a
	// duration, a proportion — has no natural unit to pick a default in, so its
	// floor stays at zero until an operator sets one.
	if p.MinDelta <= 0 && metric.CountLike {
		p.MinDelta = defaultCountMinDelta
	}
	if metric.Ratio() && p.MinDenominator <= 0 {
		p.MinDenominator = defaultMinDenominator
	}
}

func (c *spikeCondition) Kind() string   { return KindSpike }
func (c *spikeCondition) Downward() bool { return c.params.Direction == DirectionDown }

func (c *spikeCondition) Label() string {
	return fmt.Sprintf("%s %s sharply over %s%s",
		c.metric.Name, c.params.Direction, windowLabel(c.params.WindowBuckets, c.step), scopeLabel(c.spec.Scope))
}

// Query asks for the window, the guard band and the baseline in one span. The
// rolling aggregate is computed over the whole of it, so the baseline region
// needs a further WindowBuckets-1 buckets of run-up before it to be complete.
func (c *spikeCondition) Query(now time.Time) Query {
	return c.query(now, c.params.WindowBuckets+c.params.GuardBuckets+
		c.params.BaselineBuckets+c.params.WindowBuckets-1)
}

func (c *spikeCondition) Unavailable(reason, err string) Outcome {
	out := c.outcome(KindSpike, c.Label(), spikeOp(c.params.Direction), c.params.Z)
	out.Err = err
	return out.finish(Unknown, reason)
}

func spikeOp(direction string) Op {
	if direction == DirectionDown {
		return OpLT
	}
	return OpGT
}

func (c *spikeCondition) Evaluate(now time.Time, s Series) Outcome {
	out := c.outcome(KindSpike, c.Label(), spikeOp(c.params.Direction), c.params.Z)

	// The rolling series first: the observation and the baseline must be the same
	// statistic over the same width of time, or a five-minute window compared
	// against one-minute buckets finds a "spike" of exactly five every time.
	rolled := s.Rolling(c.agg, c.params.WindowBuckets, c.params.MinSamples)
	_, to := windowIndices(s, now, c.params.WindowBuckets)
	obs := to - 1
	if obs < 0 {
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}
	out.WindowFrom, out.WindowTo = s.BucketStart(obs-c.params.WindowBuckets+1), s.BucketStart(to)

	if c.metric.Ratio() {
		if denominator := s.SumDenominator(obs-c.params.WindowBuckets+1, to); denominator < c.params.MinDenominator {
			out.Denominator = denominator
			return out.finish(Unknown, ReasonSmallDenom)
		}
	}

	x, ok := rolled.At(obs)
	if !ok {
		out.NoData = true
		return out.finish(Unknown, ReasonNoData)
	}
	out.Observed = &x
	out.Samples = len(s.Known(obs-c.params.WindowBuckets+1, to))

	baselineTo := obs - c.params.GuardBuckets
	baseline := rolled.Known(baselineTo-c.params.BaselineBuckets, baselineTo)
	out.BaselineSamples = len(baseline)
	if len(baseline) < c.params.MinBaseline {
		// Not enough history to say what normal is. Reported as undecided rather
		// than as fine: an unbaselined watch is not a quiet watch, and calling it
		// ok would let a brand-new watch resolve an incident it never evaluated.
		return out.finish(Unknown, ReasonShortBaseline)
	}

	median, mad, _ := MAD(baseline)
	out.Baseline = &median
	scale := Scale(median, mad, c.metric.CountLike, c.params.MinScale)

	delta := x - median
	if c.params.Direction == DirectionDown {
		delta = -delta
	}
	z := delta / scale
	out.Score = &z

	return c.gate(out, z, delta, x, median)
}

// gate applies the three tests in the order that makes the failure most legible:
// the statistical one first, because it is the one that usually stops a change,
// then the absolute and proportional floors that stop the changes a quiet series
// makes look significant.
func (c *spikeCondition) gate(out Outcome, z, delta, observed, median float64) Outcome {
	if z < c.params.Z {
		return out.finish(False, ReasonBelowZ)
	}
	if delta < c.params.MinDelta {
		return out.finish(False, ReasonSmallDelta)
	}
	if !c.clearsRatio(observed, median) {
		return out.finish(False, ReasonSmallRatio)
	}
	return out.finish(True, ReasonConditionMet)
}

// clearsRatio asks whether the change is large in proportion as well as in
// absolute terms. A baseline at or below zero has no meaningful proportion — the
// quotient is undefined or sign-flipped — so the gate stands aside and lets the z
// and delta tests carry the decision.
func (c *spikeCondition) clearsRatio(observed, median float64) bool {
	if median <= 0 {
		return true
	}
	if c.params.Direction == DirectionDown {
		return observed*c.params.MinRatio <= median
	}
	return observed >= c.params.MinRatio*median
}
