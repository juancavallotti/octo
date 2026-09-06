package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/logs/internal/podstats"
)

// statsNow is the fixed clock the default window is measured from.
var statsNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

type fakeStatsReader struct {
	pods      []podstats.PodStatus
	truncated bool
	metrics   []podstats.Metric
	warnings  []podstats.Warning
	result    podstats.Result
	err       error

	gotQuery  podstats.Query
	gotPods   []string
	gotPrefix string
}

func (f *fakeStatsReader) Pods(context.Context, string) ([]podstats.PodStatus, bool, error) {
	return f.pods, f.truncated, f.err
}

func (f *fakeStatsReader) Metrics(_ context.Context, _ string, pods []string, prefix string) ([]podstats.Metric, []podstats.Warning, bool, error) {
	f.gotPods, f.gotPrefix = pods, prefix
	return f.metrics, f.warnings, f.truncated, f.err
}

func (f *fakeStatsReader) Series(_ context.Context, q podstats.Query) (podstats.Result, error) {
	f.gotQuery = q
	return f.result, f.err
}

func doStats(t *testing.T, r StatsReader, target string) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	h := NewStatsHandler(r)
	h.now = func() time.Time { return statsNow }
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeStats[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return out
}

func ptrOf(f float64) *float64 { return &f }

// A deployment with no stats is an empty answer, not a 404. This service holds
// no deployment registry, so it cannot tell one that never existed from one
// whose sidecar is off — and must not imply that it can.
func TestStatsUnknownDeploymentIsEmptyNot404(t *testing.T) {
	for _, target := range []string{
		"/stats/nope/pods",
		"/stats/nope/metrics",
		"/stats/nope/series?metric=go_goroutines",
	} {
		rec := doStats(t, &fakeStatsReader{}, target)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "null") &&
			!strings.Contains(rec.Body.String(), `"warnings":[]`) {
			t.Errorf("GET %s body = %s, want empty arrays rather than nulls",
				target, rec.Body.String())
		}
	}
}

func TestStatsPodsReportsBothTiers(t *testing.T) {
	started := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	rec := doStats(t, &fakeStatsReader{
		pods: []podstats.PodStatus{{
			Pod:      "octo-dep-1-abc",
			LastSeen: statsNow.Add(-3 * time.Hour),
			Meta: podstats.Meta{
				Gen:            3,
				SampleInterval: time.Second,
				RollupInterval: time.Hour,
				Retention:      168 * time.Hour,
				StartedAt:      started,
			},
			// The ordinary state of a pod that stopped hours ago: the live tier
			// expired while the history and the index entry remain.
			LiveRows:   0,
			RollupRows: 40,
			Series:     95,
		}},
	}, "/stats/dep-1/pods")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeStats[statsPodsResponse](t, rec)

	if got.DeploymentID != "dep-1" || len(got.Items) != 1 {
		t.Fatalf("response = %+v", got)
	}
	pod := got.Items[0]
	if pod.LiveRows != 0 || pod.RollupRows != 40 {
		t.Errorf("rows = %d live / %d rollup, want 0 and 40", pod.LiveRows, pod.RollupRows)
	}
	if pod.Reporting {
		t.Error("a pod last seen three hours ago is reported as reporting")
	}
	if pod.Series != 95 || pod.Generation != 3 {
		t.Errorf("pod = %+v, want 95 series at generation 3", pod)
	}
	if pod.SampleInterval != "1s" || pod.Retention != "168h0m0s" {
		t.Errorf("intervals = %s / %s", pod.SampleInterval, pod.Retention)
	}
	if pod.StartedAt == nil || *pod.StartedAt != "2026-09-05T09:00:00Z" {
		t.Errorf("startedAt = %v", pod.StartedAt)
	}
}

func TestStatsPodsOmitsAnUnknownStartTime(t *testing.T) {
	rec := doStats(t, &fakeStatsReader{
		pods: []podstats.PodStatus{{Pod: "p", Meta: podstats.Meta{SampleInterval: time.Second}}},
	}, "/stats/dep-1/pods")

	if strings.Contains(rec.Body.String(), "startedAt") {
		t.Errorf("body = %s, want no startedAt for a pod with no recorded start",
			rec.Body.String())
	}
}

func TestStatsMetricsGroupsLabelSetsUnderTheName(t *testing.T) {
	rec := doStats(t, &fakeStatsReader{
		metrics: []podstats.Metric{{
			Name: "octo_flow_latency_bucket",
			Kind: podstats.KindCounter,
			Series: []podstats.MetricSeries{
				{Labels: map[string]string{"le": "0.005"}, Pods: []string{"p1", "p2"}},
				{Labels: map[string]string{"le": "+Inf"}, Pods: []string{"p1"}},
			},
		}},
		warnings: []podstats.Warning{{Pod: "p3", Reason: "no dictionary"}},
	}, "/stats/dep-1/metrics")

	got := decodeStats[statsMetricsResponse](t, rec)
	if len(got.Items) != 1 {
		t.Fatalf("items = %+v, want one metric", got.Items)
	}
	// Spelled out, not the storage's single letter.
	if got.Items[0].Kind != "counter" {
		t.Errorf("kind = %q, want counter", got.Items[0].Kind)
	}
	if len(got.Items[0].Series) != 2 {
		t.Errorf("series = %+v, want both label sets", got.Items[0].Series)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Pod != "p3" {
		t.Errorf("warnings = %+v", got.Warnings)
	}
}

func TestStatsMetricsPassesFilters(t *testing.T) {
	fake := &fakeStatsReader{}
	doStats(t, fake, "/stats/dep-1/metrics?pod=p1&pod=p2&prefix=octo_")

	if len(fake.gotPods) != 2 || fake.gotPods[0] != "p1" {
		t.Errorf("pods = %v, want both", fake.gotPods)
	}
	if fake.gotPrefix != "octo_" {
		t.Errorf("prefix = %q, want octo_", fake.gotPrefix)
	}
}

// The metric filter is what bounds the response — the rows are positional, so
// a query with no name filter reads every series of every pod.
func TestStatsSeriesRequiresAMetric(t *testing.T) {
	for _, target := range []string{
		"/stats/dep-1/series",
		"/stats/dep-1/series?metric=",
		"/stats/dep-1/series?tier=live",
	} {
		rec := doStats(t, &fakeStatsReader{}, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "metric is required") {
			t.Errorf("GET %s body = %s, want it to name the missing parameter",
				target, rec.Body.String())
		}
	}
}

func TestStatsSeriesParsesTheQuery(t *testing.T) {
	fake := &fakeStatsReader{}
	doStats(t, fake, "/stats/dep-1/series?"+
		"metric=go_goroutines&metric=octo_flow_messages_total&"+
		"label=flow%3Da&label=le%3D0.005&"+
		"pod=p1&tier=rollup&counters=absolute&stats=value,max&limit=50&"+
		"from=2026-09-05T11%3A00%3A00Z&to=2026-09-05T11%3A30%3A00Z")

	q := fake.gotQuery
	if q.DeploymentID != "dep-1" {
		t.Errorf("deployment = %q", q.DeploymentID)
	}
	if len(q.Selector.Names) != 2 {
		t.Errorf("names = %v, want both", q.Selector.Names)
	}
	if q.Selector.Labels["flow"] != "a" || q.Selector.Labels["le"] != "0.005" {
		t.Errorf("labels = %v, want both ANDed", q.Selector.Labels)
	}
	if len(q.Pods) != 1 || q.Pods[0] != "p1" {
		t.Errorf("pods = %v", q.Pods)
	}
	if q.Tier != podstats.TierRollup {
		t.Errorf("tier = %q, want rollup", q.Tier)
	}
	if q.Projection.Counters != podstats.CountersAbsolute {
		t.Errorf("counters = %q, want absolute", q.Projection.Counters)
	}
	if len(q.Projection.Stats) != 2 || q.Projection.Stats[1] != podstats.StatMax {
		t.Errorf("stats = %v, want value and max", q.Projection.Stats)
	}
	if q.Projection.Limit != 50 {
		t.Errorf("limit = %d, want 50", q.Projection.Limit)
	}
	if !q.From.Equal(time.Date(2026, 9, 5, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("from = %v", q.From)
	}
}

// The defaults are what a caller watching something now gets without saying so.
func TestStatsSeriesDefaults(t *testing.T) {
	fake := &fakeStatsReader{}
	doStats(t, fake, "/stats/dep-1/series?metric=go_goroutines")

	q := fake.gotQuery
	if q.Tier != podstats.TierAuto {
		t.Errorf("tier = %q, want auto", q.Tier)
	}
	if q.Projection.Counters != podstats.CountersDelta {
		t.Errorf("counters = %q, want delta", q.Projection.Counters)
	}
	if len(q.Projection.Stats) != 1 || q.Projection.Stats[0] != podstats.StatValue {
		t.Errorf("stats = %v, want value alone", q.Projection.Stats)
	}
	if q.Projection.Limit != statsDefaultPoints {
		t.Errorf("limit = %d, want %d", q.Projection.Limit, statsDefaultPoints)
	}
	if !q.To.Equal(statsNow) {
		t.Errorf("to = %v, want now", q.To)
	}
	if want := statsNow.Add(-statsDefaultWindow); !q.From.Equal(want) {
		t.Errorf("from = %v, want %v", q.From, want)
	}
}

// Either bound may be given alone, as on the traces list.
func TestStatsSeriesWindowFromAlone(t *testing.T) {
	fake := &fakeStatsReader{}
	doStats(t, fake, "/stats/dep-1/series?metric=g&from=2026-09-05T11%3A00%3A00Z")

	if !fake.gotQuery.To.Equal(statsNow) {
		t.Errorf("to = %v, want now", fake.gotQuery.To)
	}
}

func TestStatsSeriesLimitIsClamped(t *testing.T) {
	for target, want := range map[string]int{
		"/stats/dep-1/series?metric=g&limit=0":     1,
		"/stats/dep-1/series?metric=g&limit=-5":    1,
		"/stats/dep-1/series?metric=g&limit=99999": statsMaxPoints,
	} {
		fake := &fakeStatsReader{}
		doStats(t, fake, target)
		if got := fake.gotQuery.Projection.Limit; got != want {
			t.Errorf("GET %s gave limit %d, want %d", target, got, want)
		}
	}
}

func TestStatsSeriesRejectsBadParameters(t *testing.T) {
	for name, target := range map[string]string{
		"tier":     "/stats/dep-1/series?metric=g&tier=hourly",
		"counters": "/stats/dep-1/series?metric=g&counters=raw",
		"stat":     "/stats/dep-1/series?metric=g&stats=value,median",
		"label":    "/stats/dep-1/series?metric=g&label=flow",
		"limit":    "/stats/dep-1/series?metric=g&limit=lots",
		"time":     "/stats/dep-1/series?metric=g&from=yesterday",
		"order":    "/stats/dep-1/series?metric=g&from=2026-09-05T12%3A00%3A00Z&to=2026-09-05T11%3A00%3A00Z",
		"too many": "/stats/dep-1/series?" + strings.Repeat("metric=g&", statsMaxMetrics+1),
	} {
		rec := doStats(t, &fakeStatsReader{}, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: GET %s = %d, want 400", name, target, rec.Code)
		}
	}
}

func TestStatsSeriesEncodesGapsAsNull(t *testing.T) {
	rec := doStats(t, &fakeStatsReader{
		result: podstats.Result{
			Tier: podstats.TierLive,
			Step: time.Second,
			Series: []podstats.Series{{
				Pod: "p1", Name: "go_goroutines", Kind: podstats.KindGauge,
				TimesMS: []int64{1000, 2000, 3000},
				Values:  []*float64{ptrOf(5), nil, ptrOf(6)},
			}},
		},
	}, "/stats/dep-1/series?metric=go_goroutines")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "[5,null,6]") {
		t.Errorf("body = %s, want the gap as null", rec.Body.String())
	}

	got := decodeStats[statsSeriesResponse](t, rec)
	if got.Tier != "live" || got.Step != "1s" {
		t.Errorf("resolved tier = %q step %q, want live and 1s", got.Tier, got.Step)
	}
	if got.Series[0].Kind != "gauge" {
		t.Errorf("kind = %q, want gauge", got.Series[0].Kind)
	}
	// Live rows have no bucket edges, so the column is omitted rather than sent
	// empty.
	if len(got.Series[0].Ends) != 0 {
		t.Errorf("ends = %v on the live tier", got.Series[0].Ends)
	}
}

// The whole response has to encode. A NaN reaching the encoder would surface as
// a 200 with a truncated body, because httpx.WriteJSON writes the status first.
func TestStatsSeriesResponseAlwaysEncodes(t *testing.T) {
	nan := math.NaN()
	rec := doStats(t, &fakeStatsReader{
		result: podstats.Result{
			Tier: podstats.TierRollup,
			Step: time.Hour,
			Series: []podstats.Series{{
				Pod: "p1", Name: "c", Kind: podstats.KindCounter,
				TimesMS: []int64{0}, EndsMS: []int64{3600000},
				// A NaN that should never get this far. If one ever does, the
				// response must still be a complete document.
				Values: []*float64{&nan},
			}},
		},
	}, "/stats/dep-1/series?metric=c")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
}

func TestStatsSeriesCarriesRollupEdgesAndStats(t *testing.T) {
	rec := doStats(t, &fakeStatsReader{
		result: podstats.Result{
			Tier: podstats.TierRollup,
			Step: time.Hour,
			Series: []podstats.Series{{
				Pod: "p1", Name: "c", Kind: podstats.KindCounter,
				TimesMS: []int64{0, 7200000},
				EndsMS:  []int64{3600000, 10800000},
				Values:  []*float64{ptrOf(1), ptrOf(2)},
				Last:    []*float64{ptrOf(10), ptrOf(12)},
				Samples: []int{3600, 3600},
			}},
			Warnings: []podstats.Warning{{Pod: "p2", Reason: "no rows in window"}},
		},
	}, "/stats/dep-1/series?metric=c&tier=rollup&stats=value,last,samples")

	got := decodeStats[statsSeriesResponse](t, rec)
	s := got.Series[0]

	// The gap between the first bucket's end and the second's start is a scrape
	// gap, and stays visible rather than being filled in.
	if len(s.Ends) != 2 || s.Ends[0] == s.Times[1] {
		t.Errorf("edges = times %v ends %v, want the missing hour visible",
			s.Times, s.Ends)
	}
	if len(s.Last) != 2 || len(s.Samples) != 2 {
		t.Errorf("projected columns missing: last=%v samples=%v", s.Last, s.Samples)
	}
	if len(got.Warnings) != 1 {
		t.Errorf("warnings = %+v, want the skipped pod named", got.Warnings)
	}
}

func TestStatsErrorsAre500WithoutDetail(t *testing.T) {
	for _, target := range []string{
		"/stats/dep-1/pods",
		"/stats/dep-1/metrics",
		"/stats/dep-1/series?metric=g",
	} {
		rec := doStats(t, &fakeStatsReader{err: errInvalid("redis is on fire")}, target)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("GET %s = %d, want 500", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "on fire") {
			t.Errorf("GET %s leaked the underlying error: %s", target, rec.Body.String())
		}
	}
}

// A catalogue built from a capped pod list is partial, and says so. Without the
// flag a caller cannot tell a metric no pod exposes from one exposed only by a
// pod the cap dropped.
func TestStatsMetricsReportsTruncation(t *testing.T) {
	rec := doStats(t, &fakeStatsReader{truncated: true}, "/stats/dep-1/metrics")

	if got := decodeStats[statsMetricsResponse](t, rec); !got.Truncated {
		t.Error("truncated is false although the pod list hit its cap")
	}
}
