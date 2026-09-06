package source

import (
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
	"github.com/juancavallotti/octo/observability/internal/podstats"
)

var gridStart = time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

func gridQuery(agg alerting.Aggregate, across alerting.Aggregate, buckets int) alerting.Query {
	return alerting.Query{
		Source: alerting.SourcePodStats, Metric: "octo_flow_messages_total", Aggregate: agg,
		Scope: alerting.Scope{DeploymentID: "d1", Across: across},
		Step:  time.Minute, From: gridStart, To: gridStart.Add(time.Duration(buckets) * time.Minute),
	}
}

func ptr(v float64) *float64 { return &v }

// podSeries builds one pod's series with a point every 30 seconds from the grid
// start, so two samples land in each one-minute bucket.
func podSeries(pod string, values ...*float64) podstats.Series {
	s := podstats.Series{Pod: pod, Name: "octo_flow_messages_total", Values: values}
	for i := range values {
		s.TimesMS = append(s.TimesMS, gridStart.Add(time.Duration(i)*30*time.Second).UnixMilli())
	}
	return s
}

func TestScopeSQLParameterisesEveryValue(t *testing.T) {
	scope := alerting.Scope{
		DeploymentID: "d1", AppName: "checkout", AppVersion: "v2",
		Levels: []string{"ERROR", "Warn"}, Search: "timeout",
	}
	where, args := scopeSQL(alerting.SourceLogs, scope, []any{"seed"})

	for _, want := range []string{"deployment_id = $2::uuid", "app_name = $3", "app_version = $4",
		"lower(level) = ANY($5)", "message ILIKE $6"} {
		if !strings.Contains(where, want) {
			t.Errorf("clause %q missing from %q", want, where)
		}
	}
	if len(args) != 6 {
		t.Fatalf("%d args, want 6", len(args))
	}
	// Levels are compared lowercased on both sides: the casing a runtime's
	// logger emits is not a distinction anybody meant to make.
	levels, ok := args[4].([]string)
	if !ok || levels[0] != "error" || levels[1] != "warn" {
		t.Errorf("levels %v, want them lowercased", args[4])
	}
	if args[5] != "%timeout%" {
		t.Errorf("search arg %v, want a wrapped substring", args[5])
	}
}

// The integration axis exists on traces and not on logs; asking for it on the
// wrong source must not produce a clause naming a column that is not there.
func TestScopeSQLKeepsPerSourceAxesApart(t *testing.T) {
	scope := alerting.Scope{IntegrationID: "i1", Levels: []string{"error"}}

	traces, _ := scopeSQL(alerting.SourceTraces, scope, nil)
	if !strings.Contains(traces, "integration_id") || strings.Contains(traces, "level") {
		t.Errorf("trace clause = %q", traces)
	}
	logs, _ := scopeSQL(alerting.SourceLogs, scope, nil)
	if strings.Contains(logs, "integration_id") || !strings.Contains(logs, "level") {
		t.Errorf("log clause = %q", logs)
	}
}

func TestProjectionsCoverTheCatalogue(t *testing.T) {
	for _, m := range alerting.Metrics(alerting.SourceTraces) {
		if _, _, err := traceProjection(m.Name, m.DefaultAggregate()); err != nil {
			t.Errorf("no projection for the trace metric %s: %v", m.Name, err)
		}
	}
	for _, m := range alerting.Metrics(alerting.SourceLogs) {
		if _, _, err := logProjection(m.Name, m.DefaultAggregate()); err != nil {
			t.Errorf("no projection for the log metric %s: %v", m.Name, err)
		}
	}
	if _, _, err := traceProjection("made_up", alerting.AggCount); err == nil {
		t.Error("an unknown metric produced a projection")
	}
}

// A ratio must come back from one scan, so the numerator and denominator can
// never disagree about which rows they counted.
func TestRatioProjectionsSelectBothCounts(t *testing.T) {
	for _, c := range []struct {
		name    string
		project projector
	}{{"traces", traceProjection}, {"logs", logProjection}} {
		selectList, ratio, err := c.project("error_rate", alerting.AggRatio)
		if err != nil {
			t.Fatalf("%s error_rate: %v", c.name, err)
		}
		if !ratio {
			t.Errorf("%s error_rate does not report itself as a ratio", c.name)
		}
		if strings.Count(selectList, "count(*)") != 2 {
			t.Errorf("%s error_rate selects %q, want both counts", c.name, selectList)
		}
	}
}

func TestDurationProjectionRefusesAnAggregateItCannotDo(t *testing.T) {
	if _, _, err := traceProjection("duration_ns", alerting.AggSum); err == nil {
		t.Error("summing a duration percentile produced SQL")
	}
	for _, agg := range []alerting.Aggregate{alerting.AggP95, alerting.AggAvg, alerting.AggMax} {
		if _, _, err := traceProjection("duration_ns", agg); err != nil {
			t.Errorf("duration_ns %s: %v", agg, err)
		}
	}
}

func TestIntervalOf(t *testing.T) {
	if got := intervalOf(5 * time.Minute); got != "300 seconds" {
		t.Errorf("interval = %q, want \"300 seconds\"", got)
	}
}

func TestRebucketCollapsesSamplesThenPods(t *testing.T) {
	// Two pods, two samples per bucket. Within a pod the samples sum; across
	// pods the default is the worst one.
	result := podstats.Result{Series: []podstats.Series{
		podSeries("a", ptr(1), ptr(1), ptr(5), ptr(5)),
		podSeries("b", ptr(2), ptr(2), ptr(1), ptr(1)),
	}}
	got := rebucket(gridQuery(alerting.AggSum, "", 2), result)

	if v, ok := got.At(0); !ok || v != 4 {
		t.Errorf("bucket 0 = (%v, %v), want (4, true) — pod b sums to 4 and is the worse", v, ok)
	}
	if v, ok := got.At(1); !ok || v != 10 {
		t.Errorf("bucket 1 = (%v, %v), want (10, true) — pod a sums to 10", v, ok)
	}
}

func TestRebucketAcrossRespectsTheChosenAggregate(t *testing.T) {
	result := podstats.Result{Series: []podstats.Series{
		podSeries("a", ptr(1), ptr(1)),
		podSeries("b", ptr(3), ptr(3)),
	}}
	// Per pod the bucket sums to 2 and 6; across pods, sum is 8.
	got := rebucket(gridQuery(alerting.AggSum, alerting.AggSum, 1), result)
	if v, ok := got.At(0); !ok || v != 8 {
		t.Errorf("summed across pods = (%v, %v), want (8, true)", v, ok)
	}
}

// A scrape gap arrives from podstats as a nil and must leave here as a nil. A
// sidecar that did not report is not a pod reading zero, and this is the one
// source where that distinction cannot be recovered later.
func TestRebucketNeverInventsAZero(t *testing.T) {
	result := podstats.Result{Series: []podstats.Series{podSeries("a", ptr(1), ptr(1), nil, nil)}}
	got := rebucket(gridQuery(alerting.AggSum, "", 2), result)

	if _, ok := got.At(1); ok {
		t.Error("a bucket with no reported samples produced a value")
	}
	if got.Filled {
		t.Error("a pod-stat series reported itself as zero-filled")
	}
}

// Totalling only the pods that happened to report understates the deployment,
// and an understated total is exactly what a downward condition fires on.
func TestRebucketSumRefusesAPartialBucket(t *testing.T) {
	result := podstats.Result{Series: []podstats.Series{
		podSeries("a", ptr(1), ptr(1), ptr(1), ptr(1)),
		podSeries("b", ptr(2), ptr(2), nil, nil),
	}}
	got := rebucket(gridQuery(alerting.AggSum, alerting.AggSum, 2), result)

	if v, ok := got.At(0); !ok || v != 6 {
		t.Errorf("bucket 0 = (%v, %v), want (6, true) — both pods reported", v, ok)
	}
	if _, ok := got.At(1); ok {
		t.Error("a sum was reported for a bucket in which one selected pod was silent")
	}
}

// A maximum over the pods that did report is still an honest answer to the
// question it was asked, so it is not held to the same rule.
func TestRebucketMaxToleratesAPartialBucket(t *testing.T) {
	result := podstats.Result{Series: []podstats.Series{
		podSeries("a", ptr(1), ptr(1), ptr(7), ptr(7)),
		podSeries("b", ptr(2), ptr(2), nil, nil),
	}}
	got := rebucket(gridQuery(alerting.AggSum, alerting.AggMax, 2), result)
	if v, ok := got.At(1); !ok || v != 14 {
		t.Errorf("bucket 1 = (%v, %v), want (14, true)", v, ok)
	}
}

func TestRebucketWithNoSeriesIsAllUnknown(t *testing.T) {
	got := rebucket(gridQuery(alerting.AggSum, "", 3), podstats.Result{})
	if got.Len() != 3 {
		t.Fatalf("length %d, want 3", got.Len())
	}
	for i := range 3 {
		if _, ok := got.At(i); ok {
			t.Errorf("bucket %d has a value with no series at all", i)
		}
	}
}

func TestFetchRefusesWhatItCannotAnswer(t *testing.T) {
	f := New(nil, nil)
	if _, err := f.Fetch(t.Context(), alerting.Query{Source: "elsewhere"}); err == nil {
		t.Error("an unknown source was fetched")
	}
	if _, err := f.Fetch(t.Context(), alerting.Query{Source: alerting.SourceTraces, Metric: "traces"}); err == nil {
		t.Error("a trace query with no database reported success")
	}
	// A pod-stat query with no deployment names the missing field rather than
	// returning an empty series that reads as a quiet deployment.
	_, err := f.Fetch(t.Context(), alerting.Query{Source: alerting.SourcePodStats, Metric: "x"})
	if err == nil {
		t.Error("a pod-stat query with no deployment reported success")
	}
}
