package series

import (
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

// family builds a MetricFamily for a test.
func family(name string, t dto.MetricType, metrics ...*dto.Metric) *dto.MetricFamily {
	return &dto.MetricFamily{Name: proto.String(name), Type: t.Enum(), Metric: metrics}
}

// labels builds label pairs from alternating name/value arguments.
func labels(kv ...string) []*dto.LabelPair {
	out := make([]*dto.LabelPair, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, &dto.LabelPair{Name: proto.String(kv[i]), Value: proto.String(kv[i+1])})
	}
	return out
}

func counter(v float64, kv ...string) *dto.Metric {
	return &dto.Metric{Label: labels(kv...), Counter: &dto.Counter{Value: proto.Float64(v)}}
}

func gauge(v float64, kv ...string) *dto.Metric {
	return &dto.Metric{Label: labels(kv...), Gauge: &dto.Gauge{Value: proto.Float64(v)}}
}

func histogram(count uint64, sum float64, bounds []float64, counts []uint64, kv ...string) *dto.Metric {
	buckets := make([]*dto.Bucket, 0, len(bounds))
	for i, b := range bounds {
		buckets = append(buckets, &dto.Bucket{
			UpperBound: proto.Float64(b), CumulativeCount: proto.Uint64(counts[i]),
		})
	}
	return &dto.Metric{Label: labels(kv...), Histogram: &dto.Histogram{
		SampleCount: proto.Uint64(count), SampleSum: proto.Float64(sum), Bucket: buckets,
	}}
}

// find returns the value for one identity in a sample, and whether it is present.
func find(t *testing.T, d *Dictionary, s Sample, name string, lbl map[string]string) (float64, bool) {
	t.Helper()
	i := d.lookup(name, lbl)
	if i == missingIndex || i >= len(s.Values) {
		return 0, false
	}
	return s.Values[i], true
}

func TestEncodeKinds(t *testing.T) {
	d := NewDictionary()
	fams := map[string]*dto.MetricFamily{
		"octo_flow_messages_total": family("octo_flow_messages_total", dto.MetricType_COUNTER,
			counter(7, "flow", "a", "outcome", "ok")),
		"octo_flow_in_flight": family("octo_flow_in_flight", dto.MetricType_GAUGE,
			gauge(3, "flow", "a")),
		"octo_flow_duration_seconds": family("octo_flow_duration_seconds", dto.MetricType_HISTOGRAM,
			histogram(10, 4.5, []float64{0.005, 0.01}, []uint64{4, 9}, "flow", "a")),
	}

	s := d.Encode(fams, 1000)

	tests := []struct {
		name   string
		metric string
		labels map[string]string
		want   float64
		kind   Kind
	}{
		{"counter", "octo_flow_messages_total", map[string]string{"flow": "a", "outcome": "ok"}, 7, KindCounter},
		{"gauge", "octo_flow_in_flight", map[string]string{"flow": "a"}, 3, KindGauge},
		{"histogram first bucket", "octo_flow_duration_seconds_bucket", map[string]string{"flow": "a", "le": "0.005"}, 4, KindCounter},
		{"histogram second bucket", "octo_flow_duration_seconds_bucket", map[string]string{"flow": "a", "le": "0.01"}, 9, KindCounter},
		{"histogram +Inf bucket", "octo_flow_duration_seconds_bucket", map[string]string{"flow": "a", "le": "+Inf"}, 10, KindCounter},
		{"histogram sum", "octo_flow_duration_seconds_sum", map[string]string{"flow": "a"}, 4.5, KindCounter},
		{"histogram count", "octo_flow_duration_seconds_count", map[string]string{"flow": "a"}, 10, KindCounter},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := find(t, d, s, tc.metric, tc.labels)
			if !ok {
				t.Fatalf("%s%v not in dictionary", tc.metric, tc.labels)
			}
			if got != tc.want {
				t.Errorf("value = %v, want %v", got, tc.want)
			}
			k, _ := d.Kind(d.lookup(tc.metric, tc.labels))
			if k != tc.kind {
				t.Errorf("kind = %q, want %q", k, tc.kind)
			}
		})
	}
}

// A summary's quantiles are gauges and its _sum/_count are counters, because a
// rank is not cumulative and a total is.
func TestEncodeSummarySplitsKinds(t *testing.T) {
	d := NewDictionary()
	m := &dto.Metric{Summary: &dto.Summary{
		SampleCount: proto.Uint64(5), SampleSum: proto.Float64(2.5),
		Quantile: []*dto.Quantile{{Quantile: proto.Float64(0.99), Value: proto.Float64(0.4)}},
	}}
	s := d.Encode(map[string]*dto.MetricFamily{
		"lat": family("lat", dto.MetricType_SUMMARY, m),
	}, 1000)

	q := d.lookup("lat", map[string]string{"quantile": "0.99"})
	if k, _ := d.Kind(q); k != KindGauge {
		t.Errorf("quantile kind = %q, want %q", k, KindGauge)
	}
	if s.Values[q] != 0.4 {
		t.Errorf("quantile value = %v, want 0.4", s.Values[q])
	}
	if k, _ := d.Kind(d.lookup("lat_count", nil)); k != KindCounter {
		t.Error("lat_count should be a counter")
	}
}

// Indices are assigned in a deterministic order, so two pods scraping the same
// runtime build the same dictionary rather than one keyed by Go's map seed.
func TestEncodeIsDeterministic(t *testing.T) {
	build := func() []Entry {
		d := NewDictionary()
		d.Encode(map[string]*dto.MetricFamily{
			"z_total": family("z_total", dto.MetricType_COUNTER, counter(1)),
			"a_total": family("a_total", dto.MetricType_COUNTER, counter(1)),
			"m_gauge": family("m_gauge", dto.MetricType_GAUGE, gauge(1)),
		}, 0)
		return d.Entries()
	}
	first := build()
	for i := 0; i < 20; i++ {
		got := build()
		if len(got) != len(first) {
			t.Fatalf("entry count drifted: %d vs %d", len(got), len(first))
		}
		for j := range got {
			if got[j].Name != first[j].Name {
				t.Fatalf("index %d = %q, want %q", j, got[j].Name, first[j].Name)
			}
		}
	}
}

// A series the dictionary knows but this scrape did not report holds NaN, so a
// gap is distinguishable from a genuine zero.
func TestEncodeAbsentSeriesIsNaN(t *testing.T) {
	d := NewDictionary()
	both := map[string]*dto.MetricFamily{
		"a_total": family("a_total", dto.MetricType_COUNTER, counter(1)),
		"b_total": family("b_total", dto.MetricType_COUNTER, counter(2)),
	}
	d.Encode(both, 0)

	s := d.Encode(map[string]*dto.MetricFamily{
		"a_total": family("a_total", dto.MetricType_COUNTER, counter(3)),
	}, 1000)

	if len(s.Values) != 2 {
		t.Fatalf("vector length = %d, want 2 (dictionary length)", len(s.Values))
	}
	if v, _ := find(t, d, s, "a_total", nil); v != 3 {
		t.Errorf("a_total = %v, want 3", v)
	}
	v, _ := find(t, d, s, "b_total", nil)
	if !math.IsNaN(v) {
		t.Errorf("absent b_total = %v, want NaN", v)
	}
}

// Indices are append-only across generations, so a reader holding the newest
// dictionary can decode every earlier sample.
func TestDictionaryGrowsWithoutRenumbering(t *testing.T) {
	d := NewDictionary()
	d.Encode(map[string]*dto.MetricFamily{
		"a_total": family("a_total", dto.MetricType_COUNTER, counter(1)),
	}, 0)
	first := d.lookup("a_total", nil)
	if !d.Dirty() {
		t.Fatal("dictionary should be dirty after interning a new identity")
	}
	d.MarkClean()

	// A scrape reporting exactly what the last one did adds nothing.
	d.Encode(map[string]*dto.MetricFamily{
		"a_total": family("a_total", dto.MetricType_COUNTER, counter(2)),
	}, 1000)
	if d.Dirty() {
		t.Error("an unchanged series set should not dirty the dictionary")
	}

	// A reload adds a flow.
	d.Encode(map[string]*dto.MetricFamily{
		"a_total": family("a_total", dto.MetricType_COUNTER, counter(3)),
		"b_total": family("b_total", dto.MetricType_COUNTER, counter(1)),
	}, 2000)
	if !d.Dirty() {
		t.Error("a new series should dirty the dictionary")
	}
	if got := d.lookup("a_total", nil); got != first {
		t.Errorf("a_total moved from index %d to %d", first, got)
	}
	if d.BumpGen() != 1 {
		t.Error("BumpGen should advance the generation")
	}
}

// Label order in the exposition is not guaranteed, and two orderings of the same
// labels are one series.
func TestIdentityKeyIgnoresLabelOrder(t *testing.T) {
	a := identityKey("m", map[string]string{"x": "1", "y": "2"})
	b := identityKey("m", map[string]string{"y": "2", "x": "1"})
	if a != b {
		t.Errorf("identity keys differ by label order:\n %q\n %q", a, b)
	}
	if same := identityKey("m", map[string]string{"x": "1=y", "": "2"}); same == a {
		t.Error("label values must not be able to forge another series' key")
	}
}

func TestFormatBound(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0.005, "0.005"},
		{1, "1"},
		{2.5, "2.5"},
		{math.Inf(1), "+Inf"},
	}
	for _, tc := range tests {
		if got := formatBound(tc.in); got != tc.want {
			t.Errorf("formatBound(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
