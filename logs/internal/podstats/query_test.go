package podstats

import (
	"encoding/json"
	"math"
	"testing"
)

// rowsOf builds newest-first raw rows, the way Redis hands them back.
func rowsOf(t *testing.T, values ...any) []json.RawMessage {
	t.Helper()

	out := make([]json.RawMessage, 0, len(values))
	for i := len(values) - 1; i >= 0; i-- {
		encoded, err := json.Marshal(values[i])
		if err != nil {
			t.Fatalf("encode row: %v", err)
		}
		out = append(out, encoded)
	}
	return out
}

// rawSample encodes a sample with gaps as null, since this package only decodes.
type rawSample struct {
	Gen    int        `json:"g"`
	TimeMS int64      `json:"t"`
	Values []*float64 `json:"v"`
}

func sampleAt(tMS int64, values ...*float64) rawSample {
	return rawSample{TimeMS: tMS, Values: values}
}

type rawBucket struct {
	Gen     int        `json:"g"`
	StartMS int64      `json:"t"`
	EndMS   int64      `json:"e"`
	Samples int        `json:"n"`
	Value   []*float64 `json:"v"`
	Min     []*float64 `json:"mn"`
	Max     []*float64 `json:"mx"`
	Last    []*float64 `json:"l"`
}

func num(f float64) *float64 { return &f }

func gaugeDict() map[int]Entry {
	return map[int]Entry{0: {Index: 0, Name: "go_goroutines", Kind: KindGauge}}
}

func counterDict() map[int]Entry {
	return map[int]Entry{0: {Index: 0, Name: "octo_flow_messages_total", Kind: KindCounter}}
}

func allStats() Projection {
	return Projection{
		Stats:    []Stat{StatValue, StatMin, StatMax, StatLast, StatSamples},
		Counters: CountersDelta,
	}
}

func TestGaugeLiveSeriesIsOldestFirst(t *testing.T) {
	rows := rowsOf(t,
		sampleAt(1000, num(5)),
		sampleAt(2000, num(7)),
		sampleAt(3000, num(6)),
	)

	got := decodeRows("pod-a", TierLive, rows, gaugeDict(), []int{0}, 1000, 3000, allStats())
	if len(got) != 1 {
		t.Fatalf("decoded %d series, want 1", len(got))
	}
	s := got[0]

	if s.Name != "go_goroutines" || s.Kind != KindGauge || s.Pod != "pod-a" {
		t.Errorf("series = %+v", s)
	}
	wantTimes := []int64{1000, 2000, 3000}
	if len(s.TimesMS) != 3 {
		t.Fatalf("times = %v, want %v", s.TimesMS, wantTimes)
	}
	for i, want := range wantTimes {
		if s.TimesMS[i] != want {
			t.Errorf("times = %v, want %v", s.TimesMS, wantTimes)
			break
		}
	}
	// A gauge is reported as read, never differenced.
	for i, want := range []float64{5, 7, 6} {
		if s.Values[i] == nil || *s.Values[i] != want {
			t.Errorf("values[%d] = %v, want %v", i, deref(s.Values[i]), want)
		}
	}
}

// A counter is cumulative, so what a caller wants charted is growth. The point
// before the window seeds the first delta, and is not itself emitted.
func TestCounterLiveSeriesIsGrowth(t *testing.T) {
	rows := rowsOf(t,
		sampleAt(1000, num(100)), // before the window: seeds only
		sampleAt(2000, num(110)),
		sampleAt(3000, num(115)),
	)

	got := decodeRows("pod-a", TierLive, rows, counterDict(), []int{0}, 2000, 3000, allStats())
	if len(got) != 1 {
		t.Fatalf("decoded %d series, want 1", len(got))
	}
	s := got[0]

	if len(s.TimesMS) != 2 || s.TimesMS[0] != 2000 {
		t.Fatalf("times = %v, want the two inside the window", s.TimesMS)
	}
	for i, want := range []float64{10, 5} {
		if s.Values[i] == nil || *s.Values[i] != want {
			t.Errorf("growth[%d] = %v, want %v", i, deref(s.Values[i]), want)
		}
	}
}

// A counter that reads lower than the one before means the process restarted.
// That is a reset, not a large negative delta.
func TestCounterResetIsNotNegative(t *testing.T) {
	rows := rowsOf(t,
		sampleAt(1000, num(100)),
		sampleAt(2000, num(5)), // restarted
		sampleAt(3000, num(9)),
	)

	got := decodeRows("pod-a", TierLive, rows, counterDict(), []int{0}, 1000, 3000, allStats())
	s := got[0]

	// The first point has nothing before it to grow from.
	if s.Values[0] != nil {
		t.Errorf("first point = %v, want null: nothing precedes it", deref(s.Values[0]))
	}
	for i, want := range []float64{5, 4} {
		if v := s.Values[i+1]; v == nil || *v != want {
			t.Errorf("growth[%d] = %v, want %v", i+1, deref(v), want)
		}
	}
}

// counters=absolute hands back the cumulative reading as stored, for a caller
// that would rather do its own arithmetic.
func TestCounterAbsoluteReportsTheReading(t *testing.T) {
	rows := rowsOf(t, sampleAt(1000, num(100)), sampleAt(2000, num(110)))

	p := allStats()
	p.Counters = CountersAbsolute
	got := decodeRows("pod-a", TierLive, rows, counterDict(), []int{0}, 1000, 2000, p)
	s := got[0]

	for i, want := range []float64{100, 110} {
		if v := s.Values[i]; v == nil || *v != want {
			t.Errorf("values[%d] = %v, want the raw reading %v", i, deref(v), want)
		}
	}
}

// A gap is null and never zero: a series that stopped being reported must not
// draw a cliff, and must never reach the JSON encoder as NaN.
func TestGapsAreNull(t *testing.T) {
	rows := rowsOf(t,
		sampleAt(1000, num(5)),
		sampleAt(2000, nil),
		sampleAt(3000, num(6)),
	)

	got := decodeRows("pod-a", TierLive, rows, gaugeDict(), []int{0}, 1000, 3000, allStats())
	s := got[0]

	if s.Values[1] != nil {
		t.Errorf("gap = %v, want null", deref(s.Values[1]))
	}
	if s.Values[0] == nil || s.Values[2] == nil {
		t.Error("readings either side of the gap went missing")
	}

	// The whole series has to survive encoding, which a NaN would not.
	if _, err := json.Marshal(s.Values); err != nil {
		t.Fatalf("the decoded series cannot be encoded: %v", err)
	}
}

// A gap breaks a counter's chain: what happened while it was unreported is
// unknown, so the next reading is not growth since the last one seen.
func TestCounterGapBreaksTheChain(t *testing.T) {
	rows := rowsOf(t,
		sampleAt(1000, num(100)),
		sampleAt(2000, nil),
		sampleAt(3000, num(200)),
	)

	got := decodeRows("pod-a", TierLive, rows, counterDict(), []int{0}, 1000, 3000, allStats())
	s := got[0]

	if s.Values[2] != nil {
		t.Errorf("growth after a gap = %v, want null rather than an invented %v",
			deref(s.Values[2]), 100.0)
	}
}

// The rollup tier stores the delta already, so its value passes through, and
// the extra columns come back only when asked for.
func TestRollupProjectsOnlyTheStatsAskedFor(t *testing.T) {
	rows := rowsOf(t, rawBucket{
		StartMS: 0, EndMS: 3600000, Samples: 12,
		Value: []*float64{num(42)}, Min: []*float64{num(1)},
		Max: []*float64{num(9)}, Last: []*float64{num(7)},
	})

	only := Projection{Stats: []Stat{StatValue}, Counters: CountersDelta}
	got := decodeRows("pod-a", TierRollup, rows, counterDict(), []int{0}, 0, 3600000, only)
	s := got[0]

	if s.Values[0] == nil || *s.Values[0] != 42 {
		t.Errorf("value = %v, want the stored delta 42", deref(s.Values[0]))
	}
	if len(s.Min) != 0 || len(s.Max) != 0 || len(s.Last) != 0 || len(s.Samples) != 0 {
		t.Errorf("unrequested stats came back: min=%v max=%v last=%v samples=%v",
			s.Min, s.Max, s.Last, s.Samples)
	}
	if len(s.EndsMS) != 1 || s.EndsMS[0] != 3600000 {
		t.Errorf("ends = %v, want the bucket end", s.EndsMS)
	}
}

func TestRollupReturnsEveryStatWhenAsked(t *testing.T) {
	rows := rowsOf(t, rawBucket{
		StartMS: 0, EndMS: 3600000, Samples: 12,
		Value: []*float64{num(42)}, Min: []*float64{num(1)},
		Max: []*float64{num(9)}, Last: []*float64{num(7)},
	})

	got := decodeRows("pod-a", TierRollup, rows, counterDict(), []int{0}, 0, 3600000, allStats())
	s := got[0]

	for name, tc := range map[string]struct {
		got  *float64
		want float64
	}{
		"min": {s.Min[0], 1}, "max": {s.Max[0], 9}, "last": {s.Last[0], 7},
	} {
		if tc.got == nil || *tc.got != tc.want {
			t.Errorf("%s = %v, want %v", name, deref(tc.got), tc.want)
		}
	}
	if s.Samples[0] != 12 {
		t.Errorf("samples = %d, want 12", s.Samples[0])
	}
}

// A bucket that never observed a series is a gap in all four columns.
func TestRollupUnobservedSeriesIsNull(t *testing.T) {
	rows := rowsOf(t, rawBucket{
		StartMS: 0, EndMS: 3600000, Samples: 12,
		Value: []*float64{nil}, Min: []*float64{nil},
		Max: []*float64{nil}, Last: []*float64{nil},
	})

	got := decodeRows("pod-a", TierRollup, rows, gaugeDict(), []int{0}, 0, 3600000, allStats())
	s := got[0]

	if s.Values[0] != nil || s.Min[0] != nil || s.Max[0] != nil || s.Last[0] != nil {
		t.Errorf("an unobserved series came back with readings: %+v", s)
	}
}

// Rollup rows are not contiguous — a scrape gap closes only the bucket that had
// data — so a caller needs both edges to see the hole rather than a fabricated
// bucket.
func TestRollupGapIsVisibleInTheEdges(t *testing.T) {
	rows := rowsOf(t,
		rawBucket{StartMS: 0, EndMS: 3600000, Samples: 12, Value: []*float64{num(1)}},
		rawBucket{StartMS: 7200000, EndMS: 10800000, Samples: 12, Value: []*float64{num(2)}},
	)

	got := decodeRows("pod-a", TierRollup, rows, gaugeDict(), []int{0}, 0, 10800000,
		Projection{Stats: []Stat{StatValue}, Counters: CountersDelta})
	s := got[0]

	if len(s.TimesMS) != 2 {
		t.Fatalf("times = %v, want two buckets and no invented third", s.TimesMS)
	}
	if s.EndsMS[0] == s.TimesMS[1] {
		t.Error("the missing hour is not visible: the first bucket's end meets " +
			"the second's start")
	}
}

// The row and the dictionary disagree about length in normal operation. Neither
// direction may panic, and neither may report a zero.
func TestShortAndLongRowsAgainstTheDictionary(t *testing.T) {
	dict := map[int]Entry{
		0: {Index: 0, Name: "a", Kind: KindGauge},
		1: {Index: 1, Name: "b", Kind: KindGauge},
		5: {Index: 5, Name: "f", Kind: KindGauge},
	}
	// Three values against a dictionary whose highest index is five: what a
	// generation-2 row looks like read against a generation-5 dictionary.
	rows := rowsOf(t, sampleAt(1000, num(1), num(2), num(3)))

	got := decodeRows("pod-a", TierLive, rows, dict, []int{0, 1, 5}, 1000, 1000, allStats())

	// The two indices the row covers come back; the one past its end is a gap,
	// so its series has a point with no value rather than being absent.
	if len(got) != 3 {
		t.Fatalf("decoded %d series, want 3", len(got))
	}
	for _, s := range got {
		if len(s.Values) != 1 {
			t.Fatalf("series %s has %d points, want 1", s.Name, len(s.Values))
		}
		switch s.Name {
		case "a", "b":
			if s.Values[0] == nil {
				t.Errorf("series %s lost its reading", s.Name)
			}
		case "f":
			if s.Values[0] != nil {
				t.Errorf("series f read %v from past the end of the row",
					deref(s.Values[0]))
			}
		}
	}
}

// An index the dictionary does not describe at all must not panic either.
func TestIndexMissingFromTheDictionary(t *testing.T) {
	rows := rowsOf(t, sampleAt(1000, num(1), num(2)))

	got := decodeRows("pod-a", TierLive, rows, gaugeDict(), []int{0, 1}, 1000, 1000, allStats())
	if len(got) != 2 {
		t.Fatalf("decoded %d series, want 2", len(got))
	}
	// The undescribed one still carries its readings; it just has no name.
	for _, s := range got {
		if len(s.TimesMS) != 1 {
			t.Errorf("series %q has %d points, want 1", s.Name, len(s.TimesMS))
		}
	}
}

func TestLimitKeepsTheNewestPoints(t *testing.T) {
	var samples []any
	for i := range 10 {
		samples = append(samples, sampleAt(int64(i)*1000, num(float64(i))))
	}
	rows := rowsOf(t, samples...)

	p := allStats()
	p.Limit = 3
	got := decodeRows("pod-a", TierLive, rows, gaugeDict(), []int{0}, 0, 9000, p)
	s := got[0]

	if len(s.TimesMS) != 3 {
		t.Fatalf("kept %d points, want 3", len(s.TimesMS))
	}
	if s.TimesMS[0] != 7000 || s.TimesMS[2] != 9000 {
		t.Errorf("kept %v, want the newest three", s.TimesMS)
	}
	if s.Values[2] == nil || *s.Values[2] != 9 {
		t.Errorf("last value = %v, want 9", deref(s.Values[2]))
	}
}

// Trimming has to cut every column by the same amount, or the columns stop
// lining up and every point after the cut reads the wrong stat.
func TestLimitTrimsEveryColumnTogether(t *testing.T) {
	var buckets []any
	for i := range 6 {
		buckets = append(buckets, rawBucket{
			StartMS: int64(i) * 3600000, EndMS: int64(i+1) * 3600000, Samples: i,
			Value: []*float64{num(float64(i))}, Min: []*float64{num(float64(i))},
			Max: []*float64{num(float64(i))}, Last: []*float64{num(float64(i))},
		})
	}
	rows := rowsOf(t, buckets...)

	p := allStats()
	p.Limit = 2
	got := decodeRows("pod-a", TierRollup, rows, gaugeDict(), []int{0}, 0, 6*3600000, p)
	s := got[0]

	n := len(s.TimesMS)
	if n != 2 {
		t.Fatalf("kept %d points, want 2", n)
	}
	for name, length := range map[string]int{
		"ends": len(s.EndsMS), "values": len(s.Values), "min": len(s.Min),
		"max": len(s.Max), "last": len(s.Last), "samples": len(s.Samples),
	} {
		if length != n {
			t.Errorf("%s has %d entries against %d times; the columns no longer "+
				"line up", name, length, n)
		}
	}
	if s.Samples[1] != 5 {
		t.Errorf("samples[1] = %d, want 5 from the newest bucket", s.Samples[1])
	}
}

func TestRowsOutsideTheWindowAreDropped(t *testing.T) {
	rows := rowsOf(t,
		sampleAt(1000, num(1)),
		sampleAt(5000, num(2)),
		sampleAt(9000, num(3)),
	)

	got := decodeRows("pod-a", TierLive, rows, gaugeDict(), []int{0}, 4000, 6000, allStats())
	s := got[0]

	if len(s.TimesMS) != 1 || s.TimesMS[0] != 5000 {
		t.Errorf("times = %v, want only the one inside the window", s.TimesMS)
	}
}

func TestNoMatchingIndicesDecodesNothing(t *testing.T) {
	rows := rowsOf(t, sampleAt(1000, num(1)))

	if got := decodeRows("pod-a", TierLive, rows, gaugeDict(), nil, 0, 2000, allStats()); got != nil {
		t.Errorf("decoded %v, want nothing", got)
	}
}

// A malformed row is skipped rather than failing the query: one unreadable row
// among thousands should cost that row, not the answer.
func TestMalformedRowIsSkipped(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"t":3000,"v":[3]}`),
		json.RawMessage(`not json`),
		json.RawMessage(`{"t":1000,"v":[1]}`),
	}

	got := decodeRows("pod-a", TierLive, rows, gaugeDict(), []int{0}, 1000, 3000, allStats())
	if len(got) != 1 {
		t.Fatalf("decoded %d series, want 1", len(got))
	}
	if len(got[0].TimesMS) != 2 {
		t.Errorf("kept %v, want the two readable rows", got[0].TimesMS)
	}
}

// An infinity cannot arrive from Redis — JSON has no literal for one and the
// writer stores it as null — so ptr's guard is defence in depth rather than a
// reachable path. It is still worth pinning: what it protects against is a NaN
// reaching encoding/json, and because httpx.WriteJSON writes the status before
// encoding, that would surface as a 200 with a truncated body rather than as
// an error a caller could act on.
func TestNonFiniteNeverBecomesAPoint(t *testing.T) {
	for name, f := range map[string]float64{
		"NaN":  math.NaN(),
		"+Inf": math.Inf(1),
		"-Inf": math.Inf(-1),
	} {
		if got := ptr(f); got != nil {
			t.Errorf("ptr(%s) = %v, want nil", name, *got)
		}
	}

	if got := ptr(0); got == nil || *got != 0 {
		t.Errorf("ptr(0) = %v, want a pointer to zero: a real reading of zero "+
			"is not a gap", got)
	}

	// The column a gap lands in still has to encode.
	if _, err := json.Marshal([]*float64{ptr(math.NaN()), ptr(1)}); err != nil {
		t.Fatalf("a column holding a gap cannot be encoded: %v", err)
	}
}

func TestSeriesAreOrderedByNameThenLabels(t *testing.T) {
	dict := map[int]Entry{
		0: {Index: 0, Name: "b_total", Kind: KindGauge},
		1: {Index: 1, Name: "a_total", Labels: map[string]string{"le": "0.5"}, Kind: KindGauge},
		2: {Index: 2, Name: "a_total", Labels: map[string]string{"le": "0.1"}, Kind: KindGauge},
	}
	rows := rowsOf(t, sampleAt(1000, num(1), num(2), num(3)))

	got := decodeRows("pod-a", TierLive, rows, dict, []int{0, 1, 2}, 1000, 1000, allStats())
	if len(got) != 3 {
		t.Fatalf("decoded %d series, want 3", len(got))
	}
	if got[0].Name != "a_total" || got[0].Labels["le"] != "0.1" {
		t.Errorf("first = %s %v, want a_total le=0.1", got[0].Name, got[0].Labels)
	}
	if got[2].Name != "b_total" {
		t.Errorf("last = %s, want b_total", got[2].Name)
	}
}

func deref(f *float64) any {
	if f == nil {
		return "null"
	}
	return *f
}

// Every rollup column goes through the same guard as the value column.
//
// nullable looks as though it hands back whatever is present, and only reads
// correctly once you know Values.At rejects a non-finite reading before saying
// one is present. That is easy to break from the At side without noticing, so
// this pins the four columns together rather than trusting the reading.
func TestEveryColumnRejectsNonFinite(t *testing.T) {
	v := Values{math.NaN(), math.Inf(1), math.Inf(-1), 3}

	for i, name := range []string{"NaN", "+Inf", "-Inf"} {
		if got := nullable(v, i); got != nil {
			t.Errorf("nullable(%s) = %v, want nil", name, *got)
		}
	}
	if got := nullable(v, 3); got == nil || *got != 3 {
		t.Errorf("nullable(3) = %v, want a pointer to 3", got)
	}
	// Past the end of the column, which is what an older-generation row looks
	// like against a newer dictionary.
	if got := nullable(v, 9); got != nil {
		t.Errorf("nullable past the end = %v, want nil", *got)
	}
}
