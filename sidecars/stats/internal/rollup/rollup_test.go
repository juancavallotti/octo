package rollup

import (
	"math"
	"testing"
	"time"

	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

// fixture builds a dictionary holding one counter and one gauge, and a helper
// that produces samples for them. Working through Encode rather than
// constructing Samples by hand keeps the kinds under test the ones the encoder
// actually assigns.
func fixture(t *testing.T) (*series.Dictionary, func(tMS int64, counter, gauge float64) series.Sample) {
	t.Helper()
	d := series.NewDictionary()
	return d, func(tMS int64, c, g float64) series.Sample {
		return d.Encode(map[string]*dto.MetricFamily{
			"c_total": {
				Name: proto.String("c_total"), Type: dto.MetricType_COUNTER.Enum(),
				Metric: []*dto.Metric{{Counter: &dto.Counter{Value: proto.Float64(c)}}},
			},
			"g": {
				Name: proto.String("g"), Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(g)}}},
			},
		}, tMS)
	}
}

func TestAlignDown(t *testing.T) {
	tests := []struct {
		name     string
		timeMS   int64
		interval time.Duration
		want     int64
	}{
		{"exact boundary is its own start", 3_600_000, time.Hour, 3_600_000},
		{"one ms before a boundary", 3_599_999, time.Hour, 0},
		{"one ms after a boundary", 3_600_001, time.Hour, 3_600_000},
		{"quarter hour grid", 3_601_000, 15 * time.Minute, 3_600_000},
		{"quarter hour mid bucket", 4_500_001, 15 * time.Minute, 4_500_000},
		{"epoch", 0, time.Hour, 0},
		// A clock that has not been set can read before the epoch; the bucket
		// must not align forwards into the future.
		{"before the epoch", -1, time.Hour, -3_600_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AlignDown(tc.timeMS, tc.interval); got != tc.want {
				t.Errorf("AlignDown(%d, %v) = %d, want %d", tc.timeMS, tc.interval, got, tc.want)
			}
		})
	}
}

// Boundaries come from the epoch, not from when sampling started, so two pods
// that start at different moments produce rows on the same grid.
func TestBucketsAlignToEpochNotStart(t *testing.T) {
	const hour = int64(3_600_000)
	dict, sample := fixture(t)

	// A pod that starts 40 minutes into the hour.
	c := NewCollector(time.Hour, dict)
	if b := c.Add(sample(hour+2_400_000, 1, 1)); b != nil {
		t.Fatal("first sample should not close a bucket")
	}
	// ...and one that starts 5 minutes in.
	other := NewCollector(time.Hour, dict)
	other.Add(sample(hour+300_000, 1, 1))

	first, _ := c.Open()
	second, _ := other.Open()
	if first != hour || second != hour {
		t.Errorf("bucket starts = %d and %d, want both %d", first, second, hour)
	}
}

// The headline rule: a counter collapses to how much it grew, not to the sum of
// its readings.
func TestCounterCollapsesToDelta(t *testing.T) {
	dict, sample := fixture(t)
	c := NewCollector(time.Hour, dict)

	for i, v := range []float64{10, 20, 30, 45} {
		c.Add(sample(int64(i)*1000, v, 0))
	}
	b := c.Close()

	idx := indexOf(t, dict, "c_total")
	if got := b.Value[idx]; got != 35 {
		t.Errorf("counter delta = %v, want 35 (45-10)", got)
	}
	if got := b.Last[idx]; got != 45 {
		t.Errorf("counter last = %v, want 45 (closing absolute value)", got)
	}
	if b.Samples != 4 {
		t.Errorf("samples = %d, want 4", b.Samples)
	}
}

// A counter that goes backwards restarted; the drop is a reset, not a negative
// delta.
func TestCounterResetIsNotNegative(t *testing.T) {
	tests := []struct {
		name    string
		reading []float64
		want    float64
	}{
		// Grows to 30, restarts, grows to 7. Growth is 30-10 then 0->7.
		{"restart mid bucket", []float64{10, 30, 0, 7}, 27},
		// After a reset the whole post-reset reading is credited, then ordinary
		// growth on top: 5 at the reset plus 4 more is 9, not 100-9 backwards.
		{"reset then grow", []float64{100, 5, 9}, 9},
		{"no reset", []float64{1, 2, 3}, 2},
		{"flat counter", []float64{5, 5, 5}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dict, sample := fixture(t)
			c := NewCollector(time.Hour, dict)
			for i, v := range tc.reading {
				c.Add(sample(int64(i)*1000, v, 0))
			}
			b := c.Close()
			if got := b.Value[indexOf(t, dict, "c_total")]; got != tc.want {
				t.Errorf("delta = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGaugeCollapsesToMeanMinMaxLast(t *testing.T) {
	dict, sample := fixture(t)
	c := NewCollector(time.Hour, dict)
	for i, v := range []float64{2, 8, 4, 6} {
		c.Add(sample(int64(i)*1000, 0, v))
	}
	b := c.Close()

	idx := indexOf(t, dict, "g")
	for _, tc := range []struct {
		what string
		got  float64
		want float64
	}{
		{"mean", b.Value[idx], 5},
		{"min", b.Min[idx], 2},
		{"max", b.Max[idx], 8},
		{"last", b.Last[idx], 6},
	} {
		if tc.got != tc.want {
			t.Errorf("gauge %s = %v, want %v", tc.what, tc.got, tc.want)
		}
	}
}

// Histogram buckets are cumulative counters and must stay cumulative through a
// collapse, or the distribution is lost.
func TestHistogramBucketsStayCumulative(t *testing.T) {
	dict := series.NewDictionary()
	hist := func(tMS int64, le005, le01, count uint64, sum float64) series.Sample {
		return dict.Encode(map[string]*dto.MetricFamily{
			"d_seconds": {
				Name: proto.String("d_seconds"), Type: dto.MetricType_HISTOGRAM.Enum(),
				Metric: []*dto.Metric{{Histogram: &dto.Histogram{
					SampleCount: proto.Uint64(count), SampleSum: proto.Float64(sum),
					Bucket: []*dto.Bucket{
						{UpperBound: proto.Float64(0.005), CumulativeCount: proto.Uint64(le005)},
						{UpperBound: proto.Float64(0.01), CumulativeCount: proto.Uint64(le01)},
					},
				}}},
			},
		}, tMS)
	}

	c := NewCollector(time.Hour, dict)
	c.Add(hist(0, 1, 2, 4, 1.0))
	c.Add(hist(1000, 5, 9, 20, 6.0))
	b := c.Close()

	tests := []struct {
		metric string
		labels map[string]string
		want   float64
	}{
		{"d_seconds_bucket", map[string]string{"le": "0.005"}, 4}, // 5-1
		{"d_seconds_bucket", map[string]string{"le": "0.01"}, 7},  // 9-2
		{"d_seconds_bucket", map[string]string{"le": "+Inf"}, 16}, // 20-4
		{"d_seconds_count", nil, 16},                              // 20-4
		{"d_seconds_sum", nil, 5.0},                               // 6.0-1.0
	}
	for _, tc := range tests {
		name := tc.metric
		if le, ok := tc.labels["le"]; ok {
			name += "{le=" + le + "}"
		}
		t.Run(name, func(t *testing.T) {
			idx := indexOfLabelled(t, dict, tc.metric, tc.labels)
			if got := b.Value[idx]; got != tc.want {
				t.Errorf("delta = %v, want %v", got, tc.want)
			}
		})
	}

	// Ordering survives too: an inner bucket can never exceed an outer one.
	inner := b.Value[indexOfLabelled(t, dict, "d_seconds_bucket", map[string]string{"le": "0.005"})]
	outer := b.Value[indexOfLabelled(t, dict, "d_seconds_bucket", map[string]string{"le": "0.01"})]
	if inner > outer {
		t.Errorf("cumulative ordering broken: le=0.005 delta %v > le=0.01 delta %v", inner, outer)
	}
}

// Crossing a boundary closes the interval that ended and opens the new one.
func TestBoundaryClosesPreviousBucket(t *testing.T) {
	const hour = int64(3_600_000)
	dict, sample := fixture(t)
	c := NewCollector(time.Hour, dict)

	c.Add(sample(1000, 10, 1))
	c.Add(sample(2000, 20, 3))

	closed := c.Add(sample(hour+1000, 30, 9))
	if closed == nil {
		t.Fatal("crossing a boundary should close a bucket")
	}
	if closed.StartMS != 0 || closed.EndMS != hour {
		t.Errorf("closed bucket = [%d,%d), want [0,%d)", closed.StartMS, closed.EndMS, hour)
	}
	if got := closed.Value[indexOf(t, dict, "c_total")]; got != 10 {
		t.Errorf("closed counter delta = %v, want 10 (the third sample belongs to the next bucket)", got)
	}

	// The sample that closed it belongs to the new bucket, not the old one.
	start, n := c.Open()
	if start != hour || n != 1 {
		t.Errorf("open bucket = start %d with %d samples, want %d with 1", start, n, hour)
	}
}

// A gap longer than a bucket emits only the bucket that has data. A row of NaNs
// for an interval nothing was recorded in asserts less than no row at all.
func TestGapSkipsEmptyBuckets(t *testing.T) {
	const hour = int64(3_600_000)
	dict, sample := fixture(t)
	c := NewCollector(time.Hour, dict)

	c.Add(sample(1000, 10, 1))
	closed := c.Add(sample(5*hour+1000, 99, 2))
	if closed == nil {
		t.Fatal("expected the first bucket to close")
	}
	if closed.StartMS != 0 {
		t.Errorf("closed bucket start = %d, want 0", closed.StartMS)
	}
	if start, _ := c.Open(); start != 5*hour {
		t.Errorf("open bucket start = %d, want %d", start, 5*hour)
	}
}

// A series the dictionary knows but the scrape stopped reporting leaves a gap,
// not a zero, and does not drag a mean down.
func TestAbsentSeriesDoesNotSkewCollapse(t *testing.T) {
	dict := series.NewDictionary()
	both := func(tMS int64, a, b float64) series.Sample {
		return dict.Encode(map[string]*dto.MetricFamily{
			"a": {Name: proto.String("a"), Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(a)}}}},
			"b": {Name: proto.String("b"), Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(b)}}}},
		}, tMS)
	}
	onlyA := func(tMS int64, a float64) series.Sample {
		return dict.Encode(map[string]*dto.MetricFamily{
			"a": {Name: proto.String("a"), Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(a)}}}},
		}, tMS)
	}

	c := NewCollector(time.Hour, dict)
	c.Add(both(0, 1, 10))
	c.Add(onlyA(1000, 3))
	c.Add(onlyA(2000, 5))
	bkt := c.Close()

	if got := bkt.Value[indexOf(t, dict, "a")]; got != 3 {
		t.Errorf("a mean = %v, want 3", got)
	}
	// b was reported once, at 10. Its mean is 10, not 10/3.
	if got := bkt.Value[indexOf(t, dict, "b")]; got != 10 {
		t.Errorf("b mean = %v, want 10 (one reading, two gaps)", got)
	}
}

// A series that never reported at all in a bucket is NaN, distinguishable from
// a genuine zero.
func TestNeverReportedIsNaN(t *testing.T) {
	dict := series.NewDictionary()
	// Teach the dictionary about "b", then never report it again.
	dict.Encode(map[string]*dto.MetricFamily{
		"a": {Name: proto.String("a"), Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(1)}}}},
		"b": {Name: proto.String("b"), Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(1)}}}},
	}, 0)

	c := NewCollector(time.Hour, dict)
	c.Add(dict.Encode(map[string]*dto.MetricFamily{
		"a": {Name: proto.String("a"), Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(4)}}}},
	}, 1000))
	b := c.Close()

	if !math.IsNaN(b.Value[indexOf(t, dict, "b")]) {
		t.Errorf("never-reported series = %v, want NaN", b.Value[indexOf(t, dict, "b")])
	}
}

// The dictionary growing mid-bucket widens the accumulators without disturbing
// what is already there.
func TestDictionaryGrowthMidBucket(t *testing.T) {
	dict := series.NewDictionary()
	c := NewCollector(time.Hour, dict)

	c.Add(dict.Encode(map[string]*dto.MetricFamily{
		"a": {Name: proto.String("a"), Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(2)}}}},
	}, 0))
	c.Add(dict.Encode(map[string]*dto.MetricFamily{
		"a": {Name: proto.String("a"), Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(4)}}}},
		"new": {Name: proto.String("new"), Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(7)}}}},
	}, 1000))
	b := c.Close()

	if len(b.Value) != dict.Len() {
		t.Fatalf("bucket width = %d, want the dictionary length %d", len(b.Value), dict.Len())
	}
	if got := b.Value[indexOf(t, dict, "a")]; got != 3 {
		t.Errorf("a mean = %v, want 3", got)
	}
	if got := b.Value[indexOf(t, dict, "new")]; got != 7 {
		t.Errorf("new mean = %v, want 7", got)
	}

	// The bucket's generation must name a dictionary that contains every index
	// the bucket holds. Growth widens the vectors, so a bucket stamped with the
	// generation it OPENED at would name one missing the indices it ends up
	// with — the same mismatch Encode avoids for samples.
	if b.Gen != dict.Gen() {
		t.Errorf("bucket gen = %d, want %d: it holds %d series and generation %d "+
			"does not describe them all", b.Gen, dict.Gen(), len(b.Value), b.Gen)
	}
}

// A bucket's generation never goes backwards, and always covers its width.
func TestBucketGenerationCoversEveryIndex(t *testing.T) {
	dict := series.NewDictionary()
	reporting := func(tMS int64, names ...string) series.Sample {
		fams := map[string]*dto.MetricFamily{}
		for _, n := range names {
			fams[n] = &dto.MetricFamily{
				Name: proto.String(n), Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(1)}}},
			}
		}
		return dict.Encode(fams, tMS)
	}

	c := NewCollector(time.Hour, dict)
	c.Add(reporting(0, "a"))
	c.Add(reporting(1000, "a", "b"))
	c.Add(reporting(2000, "a", "b", "c"))
	// A scrape that reports fewer series does not shrink the dictionary, so it
	// must not drag the generation back either.
	c.Add(reporting(3000, "a"))

	b := c.Close()
	if b.Gen != dict.Gen() {
		t.Errorf("bucket gen = %d, want the newest %d", b.Gen, dict.Gen())
	}
	if len(b.Value) != dict.Len() {
		t.Errorf("bucket holds %d series but generation %d describes %d",
			len(b.Value), b.Gen, dict.Len())
	}
}

// Close on an empty collector returns nil rather than an all-NaN row, so a
// shutdown flush with nothing pending writes nothing.
func TestCloseEmptyIsNil(t *testing.T) {
	dict, sample := fixture(t)
	c := NewCollector(time.Hour, dict)
	if b := c.Close(); b != nil {
		t.Fatalf("Close on an empty collector = %+v, want nil", b)
	}
	c.Add(sample(0, 1, 1))
	if b := c.Close(); b == nil {
		t.Fatal("Close after a sample should return a bucket")
	}
	if b := c.Close(); b != nil {
		t.Fatal("Close should leave the collector empty")
	}
}

// indexOf resolves an unlabelled series' dictionary index.
func indexOf(t *testing.T, d *series.Dictionary, name string) int {
	t.Helper()
	return indexOfLabelled(t, d, name, nil)
}

// indexOfLabelled resolves a series' dictionary index by scanning the entries,
// which is the only route from outside the package.
func indexOfLabelled(t *testing.T, d *series.Dictionary, name string, labels map[string]string) int {
	t.Helper()
	for _, e := range d.Entries() {
		if e.Name != name || len(e.Labels) != len(labels) {
			continue
		}
		match := true
		for k, v := range labels {
			if e.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			return e.Index
		}
	}
	t.Fatalf("series %s%v not in dictionary", name, labels)
	return -1
}
