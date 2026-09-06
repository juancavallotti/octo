package alerting

import (
	"testing"
	"time"
)

// ptr is the fixture helper: a series is a column of pointers, and a test that
// spelled every one of them out would be unreadable.
func ptr(v float64) *float64 { return &v }

// series builds a minute-stepped series from values, where nil is a gap.
func series(t *testing.T, start string, values ...*float64) Series {
	t.Helper()
	from, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", start, err)
	}
	return Series{Step: time.Minute, StartMS: from.UTC().UnixMilli(), Values: values}
}

func TestAlignDownFloorsToTheBucket(t *testing.T) {
	cases := []struct {
		name string
		at   string
		step time.Duration
		want string
	}{
		{"already aligned", "2026-09-06T10:00:00Z", time.Minute, "2026-09-06T10:00:00Z"},
		{"mid minute", "2026-09-06T10:00:59Z", time.Minute, "2026-09-06T10:00:00Z"},
		{"five minute", "2026-09-06T10:07:30Z", 5 * time.Minute, "2026-09-06T10:05:00Z"},
		{"hour", "2026-09-06T10:59:59Z", time.Hour, "2026-09-06T10:00:00Z"},
		{"the epoch itself", "1970-01-01T00:00:00Z", time.Hour, "1970-01-01T00:00:00Z"},
		{"just after the epoch", "1970-01-01T00:00:01Z", time.Hour, "1970-01-01T00:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, _ := time.Parse(time.RFC3339, c.at)
			want, _ := time.Parse(time.RFC3339, c.want)
			if got := AlignDown(at, c.step); !got.Equal(want) {
				t.Errorf("AlignDown(%s, %s) = %s, want %s", c.at, c.step, got.Format(time.RFC3339), c.want)
			}
		})
	}
}

// Buckets are absolute spans of time, not wall-clock hours. On the day a zone
// changes its offset, every hour bucket is still 3600 seconds — a bucket that
// silently became two hours wide would double every count inside it.
func TestAlignDownIsUnaffectedByDaylightSaving(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata available: %v", err)
	}
	// 01:30 EST on the US autumn transition, where the local hour repeats.
	local := time.Date(2026, 11, 1, 1, 30, 0, 0, ny)
	first := AlignDown(local, time.Hour)
	second := AlignDown(local.Add(time.Hour), time.Hour)
	if !second.Equal(first.Add(time.Hour)) {
		t.Errorf("hour buckets across the transition are %s and %s, not an hour apart",
			first.Format(time.RFC3339), second.Format(time.RFC3339))
	}
}

// The window must end on a bucket that closed at least EvalLag ago. A partial
// bucket is never fetched, because a series whose last point is always short is
// indistinguishable from one that dropped.
func TestWindowEndsOnAClosedBucket(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-09-06T10:07:10Z")
	from, to := Window(now, time.Minute, 5)

	wantTo, _ := time.Parse(time.RFC3339, "2026-09-06T10:05:00Z")
	if !to.Equal(wantTo) {
		t.Errorf("window ends at %s, want %s", to.Format(time.RFC3339), wantTo.Format(time.RFC3339))
	}
	if got := BucketCount(from, to, time.Minute); got != 5 {
		t.Errorf("window covers %d buckets, want 5", got)
	}
	if !to.Before(now) {
		t.Error("window ends in the future")
	}
}

func TestNewSeriesStartsEntirelyUnknown(t *testing.T) {
	from, _ := time.Parse(time.RFC3339, "2026-09-06T10:00:00Z")
	s := NewSeries(from, from.Add(3*time.Minute), time.Minute)
	if s.Len() != 3 {
		t.Fatalf("length %d, want 3", s.Len())
	}
	for i := range 3 {
		if _, ok := s.At(i); ok {
			t.Errorf("bucket %d is known before anything was written to it", i)
		}
	}
}

func TestSetAndIndexOfRoundTrip(t *testing.T) {
	from, _ := time.Parse(time.RFC3339, "2026-09-06T10:00:00Z")
	s := NewSeries(from, from.Add(3*time.Minute), time.Minute)

	i, ok := s.IndexOf(from.Add(90 * time.Second))
	if !ok || i != 1 {
		t.Fatalf("IndexOf mid-bucket = (%d, %v), want (1, true)", i, ok)
	}
	s.Set(i, 42)
	if v, known := s.At(1); !known || v != 42 {
		t.Errorf("At(1) = (%v, %v), want (42, true)", v, known)
	}
	if _, outside := s.IndexOf(from.Add(-time.Minute)); outside {
		t.Error("a time before the series reported an index")
	}
}

// Writing outside the series must not panic and must not corrupt it: window
// offsets come from operator-supplied parameters.
func TestSetOutsideTheSeriesIsIgnored(t *testing.T) {
	from, _ := time.Parse(time.RFC3339, "2026-09-06T10:00:00Z")
	s := NewSeries(from, from.Add(time.Minute), time.Minute)
	s.Set(-1, 1)
	s.Set(99, 1)
	if _, ok := s.At(0); ok {
		t.Error("an out-of-range write reached bucket 0")
	}
}

// The gap policy, stated as a table because it is the rule the whole package is
// arranged around and it is easy to regress one cell of it.
func TestFillsZero(t *testing.T) {
	cases := []struct {
		source Source
		agg    Aggregate
		want   bool
	}{
		{SourceTraces, AggCount, true},
		{SourceTraces, AggSum, true},
		{SourceTraces, AggAvg, false},
		{SourceTraces, AggP95, false},
		{SourceTraces, AggRatio, false},
		{SourceLogs, AggCount, true},
		{SourceLogs, AggRatio, false},
		// Pod stats never fill, whatever the aggregate: a missing scrape is not a
		// reading of nothing.
		{SourcePodStats, AggCount, false},
		{SourcePodStats, AggSum, false},
		{SourcePodStats, AggMax, false},
	}
	for _, c := range cases {
		if got := c.agg.FillsZero(c.source); got != c.want {
			t.Errorf("%s/%s FillsZero = %v, want %v", c.source, c.agg, got, c.want)
		}
	}
}

func TestFillZerosTurnsGapsIntoMeasurements(t *testing.T) {
	s := series(t, "2026-09-06T10:00:00Z", ptr(1), nil, ptr(3))
	s.FillZeros()
	if !s.Filled {
		t.Error("the series does not record that it was filled")
	}
	if v, ok := s.At(1); !ok || v != 0 {
		t.Errorf("filled gap = (%v, %v), want (0, true)", v, ok)
	}
}

func TestKnownDropsGaps(t *testing.T) {
	s := series(t, "2026-09-06T10:00:00Z", ptr(1), nil, ptr(3), nil, ptr(5))
	got := s.Known(0, 5)
	if len(got) != 3 {
		t.Fatalf("Known returned %d values, want 3: %v", len(got), got)
	}
	// Out-of-range bounds clamp rather than panic.
	if len(s.Known(-10, 100)) != 3 {
		t.Error("Known did not clamp its bounds")
	}
}

func TestReduce(t *testing.T) {
	values := []float64{4, 1, 3}
	cases := []struct {
		agg  Aggregate
		want float64
	}{
		{AggCount, 8}, {AggSum, 8}, {AggAvg, 8.0 / 3}, {AggMin, 1}, {AggMax, 4},
		// The worst bucket, not the mean of the buckets.
		{AggP95, 4},
	}
	for _, c := range cases {
		got, err := Reduce(c.agg, values)
		if err != nil {
			t.Fatalf("Reduce(%s): %v", c.agg, err)
		}
		closeTo(t, got, c.want, string(c.agg))
	}
}

func TestReduceRefusesWhatItCannotDoHonestly(t *testing.T) {
	if _, err := Reduce(AggCount, nil); err == nil {
		t.Error("reducing an empty window reported a number")
	}
	// A mean of per-bucket ratios weighs a two-request bucket as heavily as a
	// two-thousand-request one, so it is refused rather than approximated.
	if _, err := Reduce(AggRatio, []float64{0.5}); err == nil {
		t.Error("a ratio was reduced by averaging its quotients")
	}
	if _, err := Reduce(Aggregate("bogus"), []float64{1}); err == nil {
		t.Error("an unknown aggregate produced a number")
	}
}

func TestRollingKeepsAlignment(t *testing.T) {
	s := series(t, "2026-09-06T10:00:00Z", ptr(1), ptr(2), ptr(3), ptr(4))
	got := s.Rolling(AggSum, 2, 1)

	if got.StartMS != s.StartMS || got.Step != s.Step {
		t.Fatal("rolling moved the grid")
	}
	// Index i covers (i-window, i]: the first entry has only itself.
	want := []float64{1, 3, 5, 7}
	for i, w := range want {
		v, ok := got.At(i)
		if !ok {
			t.Fatalf("bucket %d unknown", i)
		}
		closeTo(t, v, w, "rolling sum")
	}
}

func TestRollingRespectsMinSamples(t *testing.T) {
	s := series(t, "2026-09-06T10:00:00Z", ptr(1), nil, nil, ptr(4))
	got := s.Rolling(AggSum, 3, 2)
	// Only the last window holds two known buckets... and it does not: buckets 1
	// and 2 are gaps, so (1,3] has one known value.
	for i := range 4 {
		if _, ok := got.At(i); ok {
			t.Errorf("bucket %d produced a value from fewer than two samples", i)
		}
	}
}

// A window longer than the series must produce "not enough", never a panic and
// never a number computed from a short prefix pretending to be a full window.
func TestRollingWindowLongerThanTheSeries(t *testing.T) {
	s := series(t, "2026-09-06T10:00:00Z", ptr(1), ptr(2))
	got := s.Rolling(AggSum, 10, 5)
	if got.Len() != 2 {
		t.Fatalf("length %d, want 2", got.Len())
	}
	for i := range 2 {
		if _, ok := got.At(i); ok {
			t.Errorf("bucket %d reported a full window from two buckets", i)
		}
	}
}

// The ratio is the ratio of the window, not the mean of the buckets' ratios.
// 1/10 and 9/10 is 10 in 20, not the 50%% an average of the quotients would give.
func TestRollingRatioSumsPartsNotQuotients(t *testing.T) {
	s := Series{
		Step:        time.Minute,
		Values:      []*float64{ptr(1), ptr(9)},
		Denominator: []*float64{ptr(10), ptr(10)},
	}
	got := s.Rolling(AggRatio, 2, 1)
	v, ok := got.At(1)
	if !ok {
		t.Fatal("rolling ratio produced nothing")
	}
	closeTo(t, v, 0.5, "ratio over the window")

	// And with a lopsided denominator the busy bucket dominates, which is the
	// behaviour a mean of quotients would get wrong.
	s.Denominator = []*float64{ptr(1), ptr(999)}
	s.Values = []*float64{ptr(1), ptr(0)}
	v, _ = s.Rolling(AggRatio, 2, 1).At(1)
	closeTo(t, v, 1.0/1000.0, "ratio weighted by denominator")
}

// A window in which nothing ran has no error rate. Reporting it as 0% would
// satisfy every downward condition in the product.
func TestRollingRatioWithNoDenominatorIsUndefined(t *testing.T) {
	s := Series{
		Step:        time.Minute,
		Values:      []*float64{ptr(0), ptr(0)},
		Denominator: []*float64{ptr(0), ptr(0)},
	}
	if _, ok := s.Rolling(AggRatio, 2, 1).At(1); ok {
		t.Error("a ratio over zero trials reported a value")
	}
}

func TestSumDenominator(t *testing.T) {
	s := Series{
		Step:        time.Minute,
		Values:      []*float64{ptr(1), ptr(2), ptr(3)},
		Denominator: []*float64{ptr(10), nil, ptr(30)},
	}
	closeTo(t, s.SumDenominator(0, 3), 40, "denominator total skipping the gap")
}

func TestBucketStartAndEnd(t *testing.T) {
	s := series(t, "2026-09-06T10:00:00Z", ptr(1), ptr(2))
	want, _ := time.Parse(time.RFC3339, "2026-09-06T10:01:00Z")
	if got := s.BucketStart(1); !got.Equal(want) {
		t.Errorf("BucketStart(1) = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got := s.BucketEnd(1); !got.Equal(want.Add(time.Minute)) {
		t.Errorf("BucketEnd(1) = %s, want %s", got.Format(time.RFC3339), want.Add(time.Minute).Format(time.RFC3339))
	}
}
