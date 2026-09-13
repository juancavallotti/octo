package alerting

import (
	"math"
	"sort"
)

const (
	// madToSigma converts a median absolute deviation into a standard-deviation
	// equivalent for normally distributed data. It is 1/Φ⁻¹(3/4), and it is what
	// makes a robust z-score comparable with an ordinary one, so a threshold of 4
	// means roughly what somebody expects it to mean.
	madToSigma = 1.4826

	// defaultConfidence is the interval used when a ratio condition does not name
	// one. Two-sided 95%, which is the convention everyone reads without being
	// told.
	defaultConfidence = 0.95
)

// zByConfidence is the standard normal quantile for the supported intervals.
//
// A lookup rather than an inverse-CDF approximation, because the vocabulary is
// closed and a table is exact where an approximation is merely close. An
// unsupported value falls back to 95% rather than erroring: a typo in a tuning
// knob is no reason to stop evaluating a watch.
var zByConfidence = map[float64]float64{
	0.90: 1.6449,
	0.95: 1.9600,
	0.99: 2.5758,
}

func zFor(confidence float64) float64 {
	if z, ok := zByConfidence[confidence]; ok {
		return z
	}
	return zByConfidence[defaultConfidence]
}

// Median is the middle value, averaging the two middle values of an even-length
// sample. It sorts a copy: callers pass slices they still hold.
func Median(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid], true
	}
	return (sorted[mid-1] + sorted[mid]) / 2, true
}

// MAD is the median absolute deviation from the median: the robust answer to "how
// far does this series usually move".
//
// Robust is the operative word. A standard deviation is computed from the very
// outliers an alert exists to find, so an outage inflates the spread it is judged
// against and argues itself down; a median tolerates up to half the sample being
// contaminated before the estimate moves at all.
func MAD(values []float64) (median, mad float64, ok bool) {
	median, ok = Median(values)
	if !ok {
		return 0, 0, false
	}
	deviations := make([]float64, len(values))
	for i, v := range values {
		deviations[i] = math.Abs(v - median)
	}
	mad, _ = Median(deviations)
	return median, mad, true
}

// Scale is the spread a robust z-score divides by: the MAD converted to a sigma,
// floored so it can never be zero.
//
// The floors are what make low-volume watches behave. For a quiet integration the
// MAD is frequently exactly zero — twelve buckets that all read 1, or all read 0 —
// and a z-score over that finds every subsequent point infinitely anomalous.
//
// countLike applies the Poisson floor √max(median,1): for a counting process the
// natural scale is the square root of the mean, so a baseline of 1 gets a sigma
// of at least 1 and a baseline of 100 gets at least 10. That single line is what
// stops a series that idles at one request per minute from paging on two.
//
// minScale is the operator's own floor, in the metric's units, for the cases
// where even the Poisson floor is too generous — a ratio, say, where the natural
// scale is not a count of anything.
func Scale(median, mad float64, countLike bool, minScale float64) float64 {
	scale := mad * madToSigma
	if countLike {
		scale = math.Max(scale, math.Sqrt(math.Max(median, 1)))
	}
	return math.Max(scale, minScale)
}

// WilsonLowerBound is the lower end of the Wilson score interval for a proportion
// of successes out of n trials.
//
// This is what a ratio condition compares against a threshold, rather than the raw
// k/n. One failure out of two traces is a 50% error rate by the point estimate,
// where the Wilson bound is about 9% — below any threshold anyone sets — while at
// n=400 it is about 45% and fires. The gate scales itself with the evidence.
//
// The point estimate is still what gets reported, because that is the number a
// human recognises; this is only what the comparison is made against.
func WilsonLowerBound(successes, trials, confidence float64) (float64, bool) {
	lo, _, ok := wilson(successes, trials, confidence)
	return lo, ok
}

// WilsonUpperBound is the other end, used by the downward operators. A condition
// asking whether a rate has fallen below a threshold must be as reluctant to fire
// on two samples as one asking whether it rose above one, and by symmetry that
// means comparing the upper bound.
func WilsonUpperBound(successes, trials, confidence float64) (float64, bool) {
	_, hi, ok := wilson(successes, trials, confidence)
	return hi, ok
}

func wilson(successes, trials, confidence float64) (lo, hi float64, ok bool) {
	if trials <= 0 {
		return 0, 0, false
	}
	z := zFor(confidence)
	p := successes / trials
	z2 := z * z
	denom := 1 + z2/trials
	centre := (p + z2/(2*trials)) / denom
	spread := z * math.Sqrt(p*(1-p)/trials+z2/(4*trials*trials)) / denom
	return math.Max(centre-spread, 0), math.Min(centre+spread, 1), true
}

// Bound picks the end of the interval an operator should be judged against: the
// lower bound for "is it above", the upper for "is it below". Either way the
// comparison is the conservative one, so a rate computed from very little
// evidence does not satisfy a condition in either direction.
func Bound(op Op, successes, trials, confidence float64) (float64, bool) {
	if op.Downward() {
		return WilsonUpperBound(successes, trials, confidence)
	}
	return WilsonLowerBound(successes, trials, confidence)
}
