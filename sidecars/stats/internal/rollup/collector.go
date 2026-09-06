package rollup

import (
	"time"

	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
)

// Collector folds samples into the open bucket and hands back a Bucket each
// time one closes.
//
// Not safe for concurrent use; the single sampler goroutine owns it.
type Collector struct {
	interval time.Duration
	dict     *series.Dictionary

	// startMS is the aligned start of the open bucket, or 0 before the first
	// sample has arrived and decided which bucket that is.
	startMS int64
	accs    []accumulator
	samples int
	// gen is the dictionary generation the open bucket's indices refer to. It
	// tracks the NEWEST sample folded in, not the first.
	//
	// It has to. A dictionary that grows mid-bucket widens the bucket's vectors
	// too, so a bucket stamped with the generation it opened at would name a
	// dictionary that does not contain the indices it ends up holding — the same
	// mismatch Encode avoids for samples. Advancing is safe in the other
	// direction because indices are append-only: every later generation is a
	// superset of every earlier one, so the newest resolves every index in the
	// bucket including those folded in before it existed.
	gen int
}

// NewCollector returns a Collector bucketing at interval against dict.
func NewCollector(interval time.Duration, dict *series.Dictionary) *Collector {
	return &Collector{interval: interval, dict: dict}
}

// Interval is the bucket width.
func (c *Collector) Interval() time.Duration { return c.interval }

// Open reports whether a bucket has samples in it, and the aligned start of
// that bucket. Used by diagnostics and by the shutdown flush.
func (c *Collector) Open() (startMS int64, samples int) { return c.startMS, c.samples }

// Add folds one sample in, returning the bucket the sample closed, if any.
//
// The returned bucket is the one the sample did NOT belong to: crossing a
// boundary closes the interval that just ended and opens the one the sample
// falls in. A sample that skips several intervals — the process was stopped, or
// scraping was failing for an hour — closes only the bucket that has data and
// opens the sample's own. Empty intervals are not emitted, because a row of
// NaNs asserts less than the absence of a row.
func (c *Collector) Add(s series.Sample) *Bucket {
	start := AlignDown(s.TimeMS, c.interval)

	var closed *Bucket
	switch {
	case c.samples == 0:
		// Nothing accumulated: adopt the sample's bucket, whether this is the
		// first sample ever or the first after a gap.
		c.reset(start, s.Gen)
	case start != c.startMS:
		closed = c.Close()
		c.reset(start, s.Gen)
	}

	// The dictionary may have grown since this bucket opened. Indices are
	// append-only, so widening is all that is needed and everything already
	// accumulated keeps its slot — but the bucket's generation has to move with
	// it, or it would name a dictionary missing the indices just added.
	c.grow(len(s.Values))
	if s.Gen > c.gen {
		c.gen = s.Gen
	}
	for i, v := range s.Values {
		c.accs[i].observe(v)
	}
	c.samples++
	return closed
}

// Close collapses the open bucket and returns it, or nil when nothing has been
// accumulated. It leaves the collector empty, so the caller can flush at
// shutdown without also having to reset.
func (c *Collector) Close() *Bucket {
	if c.samples == 0 {
		return nil
	}
	n := len(c.accs)
	b := &Bucket{
		Gen:     c.gen,
		StartMS: c.startMS,
		EndMS:   c.startMS + c.interval.Milliseconds(),
		Samples: c.samples,
		Value:   make([]float64, n),
		Min:     make([]float64, n),
		Max:     make([]float64, n),
		Last:    make([]float64, n),
	}
	for i := range c.accs {
		// A kind the dictionary cannot name is one whose index was appended after
		// this bucket's generation, which cannot happen for an index the bucket
		// accumulated. Falling back to the gauge rule keeps the arithmetic total.
		kind, ok := c.dict.Kind(i)
		if !ok {
			kind = series.KindGauge
		}
		b.Value[i], b.Min[i], b.Max[i], b.Last[i] = c.accs[i].collapse(kind)
	}

	c.samples, c.startMS = 0, 0
	c.accs = c.accs[:0]
	return b
}

// reset starts a fresh bucket at start.
func (c *Collector) reset(startMS int64, gen int) {
	c.startMS, c.gen, c.samples = startMS, gen, 0
	c.accs = c.accs[:0]
}

// grow widens the accumulator slice to n series, zeroing the new entries.
func (c *Collector) grow(n int) {
	for len(c.accs) < n {
		c.accs = append(c.accs, accumulator{})
	}
}

// AlignDown rounds a Unix-millisecond timestamp down to the start of the
// interval containing it, measured from the Unix epoch.
//
// From the epoch, not from process start: see the package doc. This is what
// makes two pods of one deployment produce rows on the same grid.
func AlignDown(timeMS int64, interval time.Duration) int64 {
	step := interval.Milliseconds()
	if step <= 0 {
		return timeMS
	}
	return timeMS - mod(timeMS, step)
}

// mod is a Euclidean remainder: non-negative even for a timestamp before the
// epoch, which Go's % is not. Timestamps before 1970 do not occur in a running
// pod, but a clock that has not been set yet can produce one, and a negative
// remainder there would align a bucket forwards into the future.
func mod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}
