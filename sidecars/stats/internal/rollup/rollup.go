// Package rollup turns a stream of one-second samples into two tiers: the live
// tier as scraped, and a history tier where each completed bucket is one
// collapsed row.
//
// The shape of the problem. A week of one-second samples is 604,800 rows per
// pod, which is not storable in the cache this sidecar writes to and not
// readable by anything either. A week of hourly rows is 168. So the live tier
// keeps full resolution for the current bucket and the history tier keeps one
// row per elapsed bucket, and the bucket width is configuration rather than a
// constant because the useful width is an open question — at fifteen minutes a
// week is still only 672 rows, which is a lot more resolution for very little
// more space.
//
// Bucket boundaries are aligned to wall-clock multiples of the interval since
// the Unix epoch, NOT to when the pod started. Pods of one deployment start at
// different moments and are replaced at different moments, so start-relative
// buckets would give every pod its own grid and make the rows of a deployment
// impossible to line up. Epoch-aligned, every pod that ever runs produces the
// same boundaries.
//
// # Collapsing
//
// The rule per series follows from what the series means, and two of the four
// are not the obvious choice:
//
//   - Counters collapse to the DELTA across the bucket, not the sum of their
//     readings. A Prometheus counter is cumulative — octo_flow_messages_total is
//     every message since the process started — so summing 3600 readings of it
//     would report an hour of traffic multiplied by about 1800. What is wanted
//     is how much it grew. The closing absolute value is kept alongside, because
//     that is the number that stitches consecutive buckets together.
//
//   - Histogram buckets are counters and collapse the same way, which is what
//     preserves the distribution. Averaging a histogram's buckets would report
//     the mean height of a cumulative curve, which is not a quantity.
//
//   - Gauges collapse to the mean, plus min, max and last. The extras are nearly
//     free and are the difference between a bucket that hides a spike and one
//     that shows it.
//
//   - Untyped series are treated as gauges. Averaging something that turns out
//     to be cumulative is merely uninformative; differencing something that
//     turns out not to be can go negative.
//
// # Counter resets
//
// A counter that reads lower than the previous sample means the process
// restarted and started counting from zero again. The standard handling applies:
// the drop is not a negative delta, it is a reset, and the new reading is added
// in whole. Without it a restart mid-bucket would record a large negative number
// for every counter at once.
//
// NaN marks a series the dictionary knows but the scrape did not report. It is
// skipped rather than treated as zero, so a flow removed by a config reload
// leaves a gap instead of a cliff.
package rollup

import (
	"math"

	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
)

// Bucket is one collapsed interval of the history tier.
//
// The four parallel slices are indexed by dictionary index, exactly as a
// Sample's values are, so a reader decodes a bucket against the same dictionary
// it decodes a sample against. What each slice holds depends on the series' kind
// — Value is a delta for a counter and a mean for a gauge — which is why the
// dictionary records the kind and this does not repeat it.
type Bucket struct {
	// Gen is the dictionary generation the indices refer to.
	Gen int `json:"g"`
	// StartMS is the bucket's aligned start, and the row's identity.
	StartMS int64 `json:"t"`
	// EndMS is the bucket's aligned end (exclusive).
	EndMS int64 `json:"e"`
	// Samples is how many scrapes landed in the bucket. A bucket with far fewer
	// than expected is one where scraping was failing, which is worth being able
	// to see rather than having to infer.
	Samples int `json:"n"`

	// Value is the delta for a counter, the mean for a gauge. A series the
	// bucket never observed is NaN in all four slices, written as null.
	Value series.Values `json:"v"`
	// Min, Max and Last are the extremes and the closing reading. For a counter
	// Last is its closing absolute value, which is what lets consecutive buckets
	// be stitched back into a cumulative series.
	Min  series.Values `json:"mn"`
	Max  series.Values `json:"mx"`
	Last series.Values `json:"l"`
}

// accumulator folds the samples of one bucket, one series per index.
//
// It holds fixed-size state per series rather than the samples themselves: a
// bucket is 3600 samples at the defaults, and keeping them all to aggregate at
// the end would cost the memory the dictionary encoding was introduced to save.
type accumulator struct {
	seen  bool
	first float64
	last  float64
	min   float64
	max   float64
	sum   float64
	count int
	// delta accumulates a counter's growth across resets. Held separately from
	// last-minus-first because a reset makes that difference wrong.
	delta float64
}

// observe folds one reading into the accumulator.
func (a *accumulator) observe(v float64) {
	if math.IsNaN(v) {
		return
	}
	if !a.seen {
		a.seen, a.first, a.min, a.max = true, v, v, v
	} else {
		if v < a.min {
			a.min = v
		}
		if v > a.max {
			a.max = v
		}
		// A counter that went backwards restarted from zero, so its growth since
		// the previous reading is the whole new value rather than the difference.
		if v < a.last {
			a.delta += v
		} else {
			a.delta += v - a.last
		}
	}
	a.last = v
	a.sum += v
	a.count++
}

// collapse produces the four stored numbers for one series.
func (a *accumulator) collapse(kind series.Kind) (value, minimum, maximum, last float64) {
	if !a.seen {
		nan := math.NaN()
		return nan, nan, nan, nan
	}
	if kind == series.KindCounter {
		return a.delta, a.min, a.max, a.last
	}
	return a.sum / float64(a.count), a.min, a.max, a.last
}
