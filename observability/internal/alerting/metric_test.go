package alerting

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestLookupMetric(t *testing.T) {
	m, err := LookupMetric(SourceTraces, "error_rate")
	if err != nil {
		t.Fatalf("error_rate: %v", err)
	}
	if !m.Ratio() || m.Numerator != "failed_traces" || m.Denominator != "traces" {
		t.Errorf("error_rate is not described as a ratio of failures over traces: %+v", m)
	}
	if m.CountLike {
		t.Error("a proportion must not take the Poisson floor")
	}

	// The same name means different things per source, which is why the
	// catalogues are keyed within a source rather than globally.
	logRate, err := LookupMetric(SourceLogs, "error_rate")
	if err != nil {
		t.Fatalf("log error_rate: %v", err)
	}
	if logRate.Numerator == m.Numerator {
		t.Error("the trace and log error rates resolve to the same numerator")
	}

	if _, err := LookupMetric(SourceTraces, "no_such_metric"); err == nil {
		t.Error("an unknown trace metric resolved")
	}
	if _, err := LookupMetric(Source("elsewhere"), "x"); !errors.Is(err, ErrUnknownSource) {
		t.Errorf("unknown source error = %v, want ErrUnknownSource", err)
	}
}

// Pod-stat names come from whatever the runtime exports, so they are synthesized
// rather than enumerated — but a counter still has to be recognised as one, or
// the Poisson floor never applies to the metrics that most need it.
func TestPodStatMetricsAreOpenButTyped(t *testing.T) {
	m, err := LookupMetric(SourcePodStats, "octo_flow_messages_total")
	if err != nil {
		t.Fatalf("pod stat metric: %v", err)
	}
	if !m.CountLike {
		t.Error("a _total is not treated as count-like")
	}
	gauge, _ := LookupMetric(SourcePodStats, "go_memstats_heap_inuse_bytes")
	if gauge.CountLike {
		t.Error("a gauge was treated as count-like")
	}
	if _, err := LookupMetric(SourcePodStats, ""); err == nil {
		t.Error("a pod-stat condition with no metric name was accepted")
	}
	if Metrics(SourcePodStats) != nil {
		t.Error("pod-stat metrics must not be enumerated here; they are discovered per deployment")
	}
}

func TestMetricsListsTheClosedCatalogues(t *testing.T) {
	if len(Metrics(SourceTraces)) == 0 || len(Metrics(SourceLogs)) == 0 {
		t.Error("a closed catalogue came back empty")
	}
}

func TestNewConditionValidates(t *testing.T) {
	cases := []struct {
		name string
		spec ConditionSpec
		want error
	}{
		{
			name: "no id",
			spec: ConditionSpec{Type: KindThreshold, Source: SourceTraces, Metric: "traces"},
			want: ErrInvalidParams,
		},
		{
			// The one bug in this design that could ship silently and page
			// nobody: a condition stored by a newer version must not be skipped.
			name: "unknown kind",
			spec: ConditionSpec{ID: "c", Type: "sorcery", Source: SourceTraces, Metric: "traces"},
			want: ErrUnknownCondition,
		},
		{
			name: "a group",
			spec: ConditionSpec{ID: "g", Type: KindGroup, Source: SourceTraces, Metric: "traces"},
			want: ErrNestedConditions,
		},
		{
			name: "children on a leaf",
			spec: ConditionSpec{ID: "c", Type: KindThreshold, Source: SourceTraces, Metric: "traces",
				Conditions: []ConditionSpec{{ID: "d"}}},
			want: ErrNestedConditions,
		},
		{
			name: "an aggregate the metric does not take",
			spec: ConditionSpec{ID: "c", Type: KindThreshold, Source: SourceTraces,
				Metric: "cost_usd", Aggregate: AggP95,
				Params: json.RawMessage(`{"op":"gt","threshold":1}`)},
			want: ErrInvalidParams,
		},
		{
			name: "no operator",
			spec: ConditionSpec{ID: "c", Type: KindThreshold, Source: SourceTraces, Metric: "traces",
				Params: json.RawMessage(`{"threshold":1}`)},
			want: ErrInvalidParams,
		},
		{
			// A misspelled parameter that decoded permissively would leave a
			// threshold at zero, silently, forever.
			name: "a misspelled parameter",
			spec: ConditionSpec{ID: "c", Type: KindThreshold, Source: SourceTraces, Metric: "traces",
				Params: json.RawMessage(`{"op":"gt","treshold":5}`)},
			want: ErrInvalidParams,
		},
		{
			name: "minSamples beyond the window",
			spec: ConditionSpec{ID: "c", Type: KindThreshold, Source: SourceTraces, Metric: "traces",
				Params: json.RawMessage(`{"op":"gt","threshold":1,"windowBuckets":3,"minSamples":9}`)},
			want: ErrInvalidParams,
		},
		{
			name: "a spike in no direction anyone can name",
			spec: ConditionSpec{ID: "c", Type: KindSpike, Source: SourceTraces, Metric: "traces",
				Params: json.RawMessage(`{"direction":"sideways"}`)},
			want: ErrInvalidParams,
		},
		{
			name: "absence needing more baseline than it looks at",
			spec: ConditionSpec{ID: "c", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
				Params: json.RawMessage(`{"baselineBuckets":5,"minBaseline":50}`)},
			want: ErrInvalidParams,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewCondition(c.spec, time.Minute)
			if !errors.Is(err, c.want) {
				t.Errorf("error = %v, want %v", err, c.want)
			}
		})
	}
}

func TestNewConditionFillsDefaults(t *testing.T) {
	c, err := NewCondition(ConditionSpec{
		ID: "c", Type: KindThreshold, Source: SourceTraces, Metric: "duration_ns",
		Params: json.RawMessage(`{"op":"gt","threshold":1}`),
	}, time.Minute)
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	// The default aggregate is the metric's first, which for a duration is p95
	// rather than a mean nobody asked for.
	q := c.Query(nowAfter(10, time.Minute))
	if q.Aggregate != AggP95 {
		t.Errorf("default aggregate %s, want %s", q.Aggregate, AggP95)
	}
	if q.Buckets() != defaultThresholdWindow {
		t.Errorf("default window %d buckets, want %d", q.Buckets(), defaultThresholdWindow)
	}
}

// A spike must ask for its window, its guard band, its baseline and the run-up
// the rolling aggregate needs — in one span, so the fetch is one index scan.
func TestSpikeQueryCoversItsWholeSpan(t *testing.T) {
	c, err := NewCondition(ConditionSpec{
		ID: "c", Type: KindSpike, Source: SourceTraces, Metric: "traces",
		Params: json.RawMessage(`{"windowBuckets":5,"guardBuckets":2,"baselineBuckets":20}`),
	}, time.Minute)
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if got, want := c.Query(nowAfter(50, time.Minute)).Buckets(), 5+2+20+4; got != want {
		t.Errorf("span %d buckets, want %d", got, want)
	}
}

func TestLabelsDescribeTheCondition(t *testing.T) {
	c, _ := NewCondition(ConditionSpec{
		ID: "c", Type: KindThreshold, Source: SourceTraces, Metric: "error_rate",
		Scope:  Scope{AppName: "checkout"},
		Params: json.RawMessage(`{"op":"gt","threshold":0.05,"windowBuckets":15}`),
	}, time.Minute)
	if got := c.Label(); got != "error_rate gt over 15m0s, app checkout" {
		t.Errorf("label = %q", got)
	}
}
