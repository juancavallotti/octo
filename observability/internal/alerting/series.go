package alerting

import (
	"fmt"
	"time"
)

// Series is an evenly spaced, gap-preserving column of bucket values.
//
// Columnar and aligned rather than a list of (time, value) pairs, because every
// condition in this package is positional arithmetic over a fixed grid — a window
// is a slice, a baseline is an earlier slice, a guard band is the gap between
// them. A pair list would have each condition re-deriving the grid, and the grid
// is exactly where an off-by-one bucket hides without changing any answer enough
// to look wrong.
//
// Values[i] == nil means unknown, never zero; whether an absent bucket became a
// zero or stayed nil was decided by Aggregate.FillsZero at fetch time, and Filled
// records which happened so a reader does not have to re-derive it.
//
// Denominator is populated only for ratio metrics, and is fetched in the same
// round trip as Values so the two can never disagree about which rows they
// counted. On such a series Values holds the NUMERATOR count rather than the
// quotient — the quotient only exists over a window, because a ratio is summed
// from its parts and never averaged from its buckets, so producing one per bucket
// would invite exactly the arithmetic Reduce refuses.
type Series struct {
	Step        time.Duration
	StartMS     int64
	Values      []*float64
	Denominator []*float64
	Filled      bool
}

// Len is the number of buckets.
func (s Series) Len() int { return len(s.Values) }

// At returns the value of bucket i, and whether it is known. An index outside the
// series is unknown rather than a panic: conditions compute window offsets from
// operator-supplied parameters, and a watch configured with a window longer than
// its history must produce "insufficient", not take the tick down.
func (s Series) At(i int) (float64, bool) {
	if i < 0 || i >= len(s.Values) || s.Values[i] == nil {
		return 0, false
	}
	return *s.Values[i], true
}

// DenominatorAt returns bucket i's denominator, and whether it is known.
func (s Series) DenominatorAt(i int) (float64, bool) {
	if i < 0 || i >= len(s.Denominator) || s.Denominator[i] == nil {
		return 0, false
	}
	return *s.Denominator[i], true
}

// BucketStart is when bucket i opened.
func (s Series) BucketStart(i int) time.Time {
	return time.UnixMilli(s.StartMS + int64(i)*s.Step.Milliseconds()).UTC()
}

// BucketEnd is when bucket i closed, which is when the next one opened.
func (s Series) BucketEnd(i int) time.Time { return s.BucketStart(i + 1) }

// Known returns the known values in [from, to), oldest first, dropping the gaps.
// The count of what came back is what a minimum-sample check is made against.
func (s Series) Known(from, to int) []float64 {
	if from < 0 {
		from = 0
	}
	if to > len(s.Values) {
		to = len(s.Values)
	}
	out := make([]float64, 0, maxInt(to-from, 0))
	for i := from; i < to; i++ {
		if v, ok := s.At(i); ok {
			out = append(out, v)
		}
	}
	return out
}

// SumDenominator totals the known denominators in [from, to). A ratio condition
// checks this before it will fire: a rate computed over three requests is
// arithmetic, not evidence.
func (s Series) SumDenominator(from, to int) float64 {
	if from < 0 {
		from = 0
	}
	if to > len(s.Denominator) {
		to = len(s.Denominator)
	}
	total := 0.0
	for i := from; i < to; i++ {
		if v, ok := s.DenominatorAt(i); ok {
			total += v
		}
	}
	return total
}

// Reduce collapses a window of known bucket values into one number under agg.
//
// A ratio is not reducible this way and is rejected: summing per-bucket ratios
// and dividing by the count is a mean of ratios, which is not the ratio of the
// window and is wrong in exactly the direction that matters — a bucket with two
// requests weighs as much as one with two thousand. Rolling handles ratios by
// summing the numerators and denominators separately, which is the real thing.
func Reduce(agg Aggregate, values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, ErrNoSamples
	}
	switch agg {
	case AggCount, AggSum:
		total := 0.0
		for _, v := range values {
			total += v
		}
		return total, nil
	case AggAvg:
		total := 0.0
		for _, v := range values {
			total += v
		}
		return total / float64(len(values)), nil
	case AggMin:
		out := values[0]
		for _, v := range values[1:] {
			if v < out {
				out = v
			}
		}
		return out, nil
	case AggMax:
		out := values[0]
		for _, v := range values[1:] {
			if v > out {
				out = v
			}
		}
		return out, nil
	case AggP95:
		// The worst bucket, not the mean of the buckets. Each bucket already
		// carries a percentile computed over its own rows, and averaging
		// percentiles does not produce a percentile of anything. Taking the
		// maximum answers the question a latency watch is actually asking — was
		// any part of this window slow — and it is the only reduction here that
		// cannot understate the window.
		out := values[0]
		for _, v := range values[1:] {
			if v > out {
				out = v
			}
		}
		return out, nil
	case AggRatio:
		return 0, fmt.Errorf("alerting: %w: a ratio is reduced by summing its parts, not its quotients", ErrNotReducible)
	default:
		return 0, fmt.Errorf("alerting: %w: %q", ErrUnknownAggregate, agg)
	}
}

// Rolling returns the series of windowed aggregates: index i is agg over buckets
// (i-window, i], nil where fewer than minSamples of them are known.
//
// The conditions run on this rather than on raw buckets, and that is what makes a
// spike's baseline comparable with its observation: both are the same statistic
// over the same width of time. Comparing a five-minute window against a baseline
// of one-minute buckets would find a "spike" of exactly five every time.
//
// The result keeps the input's alignment — index i still covers the bucket
// starting at StartMS + i*Step — so a caller reading the newest entry is reading
// the window that ends at the newest closed bucket.
func (s Series) Rolling(agg Aggregate, window, minSamples int) Series {
	if window < 1 {
		window = 1
	}
	if minSamples < 1 {
		minSamples = 1
	}
	out := Series{Step: s.Step, StartMS: s.StartMS, Values: make([]*float64, len(s.Values)), Filled: s.Filled}
	for i := range s.Values {
		from := i - window + 1
		if agg == AggRatio {
			out.Values[i] = s.rollingRatio(from, i+1, minSamples)
			continue
		}
		known := s.Known(from, i+1)
		if len(known) < minSamples {
			continue
		}
		v, err := Reduce(agg, known)
		if err != nil {
			continue
		}
		out.Values[i] = &v
	}
	return out
}

// rollingRatio sums the numerators and denominators across [from, to) and
// divides, which is the ratio of the window rather than the mean of its buckets.
// A zero denominator is undefined and stays nil — never 0%, which would satisfy
// every downward condition in the product for a window in which nothing ran.
func (s Series) rollingRatio(from, to, minSamples int) *float64 {
	if from < 0 {
		from = 0
	}
	if to > len(s.Values) {
		to = len(s.Values)
	}
	var num, den float64
	seen := 0
	for i := from; i < to; i++ {
		n, nok := s.At(i)
		d, dok := s.DenominatorAt(i)
		if !nok || !dok {
			continue
		}
		num += n
		den += d
		seen++
	}
	if seen < minSamples || den == 0 {
		return nil
	}
	v := num / den
	return &v
}

// AlignDown floors t to the start of the bucket of the given width.
//
// Boundaries are multiples of the step from the Unix epoch in UTC, which is what
// date_bin does on the Postgres side, so a bucket built here and a bucket built
// in SQL are the same bucket. Aligning in UTC rather than in a local zone also
// means a step never shifts across a daylight-saving boundary: an hour bucket is
// always 3600 seconds, and the day the clocks change has 23 or 25 of them rather
// than one that is silently twice as wide.
func AlignDown(t time.Time, step time.Duration) time.Time {
	if step <= 0 {
		return t.UTC()
	}
	ns := t.UTC().UnixNano()
	rem := ns % int64(step)
	if rem < 0 {
		rem += int64(step)
	}
	return time.Unix(0, ns-rem).UTC()
}

// Window is the span of closed buckets an evaluation may read at time now.
//
// The end is the last bucket to have closed at least EvalLag ago, and the start
// is buckets before it. Nothing after the end is fetched at all rather than
// fetched and discarded, so a partial bucket never reaches the arithmetic in the
// first place.
//
// Both bounds are bucket starts, and the range is half-open: [from, to) covers
// exactly the buckets whose values are returned.
func Window(now time.Time, step time.Duration, buckets int) (from, to time.Time) {
	if buckets < 1 {
		buckets = 1
	}
	to = AlignDown(now.Add(-EvalLag), step)
	from = to.Add(-time.Duration(buckets) * step)
	return from, to
}

// BucketCount is how many buckets of the given width fit in [from, to).
func BucketCount(from, to time.Time, step time.Duration) int {
	if step <= 0 || !to.After(from) {
		return 0
	}
	return int(to.Sub(from) / step)
}

// NewSeries allocates an all-unknown series covering [from, to).
//
// Fetchers build one of these and then write the buckets their query returned
// into it, which is what makes an absent row an absent bucket by default. Filling
// is then a deliberate second step taken only where Aggregate.FillsZero says
// absence was itself an observation.
func NewSeries(from, to time.Time, step time.Duration) Series {
	n := BucketCount(from, to, step)
	return Series{Step: step, StartMS: from.UTC().UnixMilli(), Values: make([]*float64, n)}
}

// IndexOf returns the bucket t falls in, and whether it is inside the series.
func (s Series) IndexOf(t time.Time) (int, bool) {
	if s.Step <= 0 {
		return 0, false
	}
	i := int((t.UTC().UnixMilli() - s.StartMS) / s.Step.Milliseconds())
	if i < 0 || i >= len(s.Values) {
		return 0, false
	}
	return i, true
}

// Set writes a known value into bucket i.
func (s *Series) Set(i int, v float64) {
	if i < 0 || i >= len(s.Values) {
		return
	}
	s.Values[i] = &v
}

// SetRatio writes a numerator and denominator into bucket i, allocating the
// denominator column on first use so a non-ratio series never carries one.
func (s *Series) SetRatio(i int, num, den float64) {
	if i < 0 || i >= len(s.Values) {
		return
	}
	if s.Denominator == nil {
		s.Denominator = make([]*float64, len(s.Values))
	}
	s.Values[i] = &num
	s.Denominator[i] = &den
}

// FillZeros records every unknown bucket as an observed zero, and marks the
// series as having been filled.
//
// Only ever called where Aggregate.FillsZero agreed, and the distinction it
// preserves is the one this whole file is arranged around: a count of zero is a
// measurement, and a nil is the absence of one.
func (s *Series) FillZeros() {
	for i := range s.Values {
		if s.Values[i] == nil {
			zero := 0.0
			s.Values[i] = &zero
		}
	}
	s.Filled = true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
