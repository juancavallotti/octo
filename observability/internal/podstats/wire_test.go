package podstats

import (
	"encoding/json"
	"math"
	"testing"
)

// A gap has to survive the decode as a gap. Reading null as zero would turn
// "this series was not reported" into "this series read zero", which is a
// flat line on a chart where there should be a hole.
func TestValuesDecodeNullAsGap(t *testing.T) {
	var s Sample
	if err := json.Unmarshal([]byte(`{"g":3,"t":1700000000000,"v":[1.5,null,3]}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if s.Gen != 3 || s.TimeMS != 1700000000000 {
		t.Errorf("header = gen %d at %d, want gen 3 at 1700000000000", s.Gen, s.TimeMS)
	}
	if len(s.Values) != 3 {
		t.Fatalf("decoded %d values, want 3", len(s.Values))
	}
	if !math.IsNaN(s.Values[1]) {
		t.Errorf("null decoded to %v, want NaN", s.Values[1])
	}

	if v, ok := s.Values.At(0); !ok || v != 1.5 {
		t.Errorf("At(0) = %v, %v; want 1.5, true", v, ok)
	}
	if _, ok := s.Values.At(1); ok {
		t.Error("At(1) reported a gap as a real reading")
	}
}

// A bucket's four slices decode the same way, and an unobserved series is a
// gap in all of them.
func TestBucketDecodes(t *testing.T) {
	const row = `{"g":1,"t":0,"e":3600000,"n":12,` +
		`"v":[1,null],"mn":[1,null],"mx":[2,null],"l":[2,null]}`

	var b Bucket
	if err := json.Unmarshal([]byte(row), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if b.EndMS != 3600000 || b.Samples != 12 {
		t.Errorf("bucket = %+v, want end 3600000 with 12 samples", b)
	}
	for name, column := range map[string]Values{
		"v": b.Value, "mn": b.Min, "mx": b.Max, "l": b.Last,
	} {
		if len(column) != 2 {
			t.Errorf("%s has %d entries, want 2", name, len(column))
			continue
		}
		if _, ok := column.At(1); ok {
			t.Errorf("%s[1] reported a gap as a real reading", name)
		}
	}
}

// The row and the dictionary genuinely disagree about length in normal
// operation, so an index outside the row is a gap and never a panic.
func TestAtIsBoundsChecked(t *testing.T) {
	// A generation-2 row read against a generation-5 dictionary is short.
	short := Values{1, 2, 3}
	for _, i := range []int{3, 5, 99} {
		if _, ok := short.At(i); ok {
			t.Errorf("At(%d) on a %d-value row reported a reading", i, len(short))
		}
	}
	if _, ok := short.At(-1); ok {
		t.Error("At(-1) reported a reading")
	}
	if _, ok := Values(nil).At(0); ok {
		t.Error("At(0) on a nil row reported a reading")
	}
}

// An infinity is not a measurement. The writer stores it as null; a reader that
// somehow meets one anyway must not hand it to a JSON encoder.
func TestInfinityIsAGap(t *testing.T) {
	v := Values{math.Inf(1), math.Inf(-1)}
	for i := range v {
		if _, ok := v.At(i); ok {
			t.Errorf("At(%d) reported an infinity as a reading", i)
		}
	}
}

// An unlabelled series has no "l" key at all, and must decode to a nil map
// rather than an empty one — the API omits labels entirely for those.
func TestEntryWithoutLabels(t *testing.T) {
	var e Entry
	if err := json.Unmarshal([]byte(`{"i":4,"n":"go_goroutines","k":"g"}`), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Index != 4 || e.Name != "go_goroutines" || e.Kind != KindGauge {
		t.Errorf("entry = %+v, want index 4, go_goroutines, gauge", e)
	}
	if e.Labels != nil {
		t.Errorf("labels = %v, want nil", e.Labels)
	}
}

func TestEntryWithLabels(t *testing.T) {
	var e Entry
	const raw = `{"i":7,"n":"octo_flow_latency_bucket","l":{"flow":"a","le":"0.005"},"k":"c"}`
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Kind != KindCounter {
		t.Errorf("kind = %q, want counter", e.Kind)
	}
	if e.Labels["le"] != "0.005" || e.Labels["flow"] != "a" {
		t.Errorf("labels = %v, want flow=a le=0.005", e.Labels)
	}
}

// The API spells kinds out; the storage uses single letters. Callers should
// never see the letters.
func TestKindString(t *testing.T) {
	for kind, want := range map[Kind]string{
		KindCounter: "counter",
		KindGauge:   "gauge",
		KindUntyped: "untyped",
		Kind("z"):   "unknown",
		Kind(""):    "unknown",
	} {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%q).String() = %q, want %q", string(kind), got, want)
		}
	}
}
