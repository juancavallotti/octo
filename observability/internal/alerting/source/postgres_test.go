package source

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// This suite writes under deployments of its own so its rows cannot appear in
// another suite's answers, and it works in a window far from any other's.
const (
	depAlerts   = "88888888-8888-8888-8888-888888888888"
	depOther    = "88888888-8888-8888-8888-888888888889"
	intAlerts   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	fetchWindow = 5
)

// seedStart is bucket zero of every fixture here, aligned to the minute so the
// Go-side grid and date_bin agree without anything having to be rounded.
var seedStart = time.Date(2033, 5, 1, 12, 0, 0, 0, time.UTC)

func newSourcePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run alerting source tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, dep := range []string{depAlerts, depOther} {
			_, _ = pool.Exec(ctx, `DELETE FROM trace_summaries WHERE deployment_id = $1::uuid`, dep)
			_, _ = pool.Exec(ctx, `DELETE FROM logs WHERE deployment_id = $1::uuid`, dep)
		}
		pool.Close()
	})
	return pool
}

// summary seeds one trace summary in the given bucket.
func summary(t *testing.T, pool *pgxpool.Pool, bucket int, status string, opts ...func(*summaryRow)) {
	t.Helper()
	row := summaryRow{
		deployment: depAlerts, integration: intAlerts, app: "checkout", version: "v1",
		durationNS: 1_000_000, cost: 0.01, llmCalls: 1, inputTokens: 10, outputTokens: 5,
	}
	for _, opt := range opts {
		opt(&row)
	}
	at := seedStart.Add(time.Duration(bucket) * time.Minute).Add(time.Second)
	// integration_id is nullable, and a summary whose deployment could not be
	// resolved to one is the ordinary case the ingest resolver leaves behind.
	var integration any
	if row.integration != "" {
		integration = row.integration
	}
	_, err := pool.Exec(t.Context(), `
		INSERT INTO trace_summaries (trace_id, deployment_id, integration_id, app_name, app_version,
		                             started_at, ended_at, status, root_duration_ns, llm_calls,
		                             input_tokens, output_tokens, cost_usd)
		VALUES (gen_random_uuid()::text, $1::uuid, $2::uuid, $3, $4, $5, $5, $6, $7, $8, $9, $10, $11)`,
		row.deployment, integration, row.app, row.version, at, status,
		row.durationNS, row.llmCalls, row.inputTokens, row.outputTokens, row.cost)
	if err != nil {
		t.Fatalf("seed a trace summary: %v", err)
	}
}

type summaryRow struct {
	deployment, integration, app, version string
	durationNS, llmCalls                  int64
	inputTokens, outputTokens             int64
	cost                                  float64
}

func inApp(name string) func(*summaryRow) { return func(r *summaryRow) { r.app = name } }
func inDeployment(id string) func(*summaryRow) {
	return func(r *summaryRow) { r.deployment = id; r.integration = "" }
}
func lasting(ns int64) func(*summaryRow) { return func(r *summaryRow) { r.durationNS = ns } }

func logRow(t *testing.T, pool *pgxpool.Pool, bucket int, level, message string) {
	t.Helper()
	at := seedStart.Add(time.Duration(bucket) * time.Minute).Add(time.Second)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO logs (deployment_id, app_name, app_version, ts, level, message)
		VALUES ($1::uuid, 'checkout', 'v1', $2, $3, $4)`, depAlerts, at, level, message)
	if err != nil {
		t.Fatalf("seed a log row: %v", err)
	}
}

// fetch runs one query over the seeded window.
func fetch(t *testing.T, pool *pgxpool.Pool, q alerting.Query) alerting.Series {
	t.Helper()
	q.Step = time.Minute
	q.From = seedStart
	q.To = seedStart.Add(fetchWindow * time.Minute)
	got, err := New(pool, nil).Fetch(t.Context(), q)
	if err != nil {
		t.Fatalf("fetch %s/%s: %v", q.Source, q.Metric, err)
	}
	if got.Len() != fetchWindow {
		t.Fatalf("series has %d buckets, want %d", got.Len(), fetchWindow)
	}
	return got
}

func wantBucket(t *testing.T, s alerting.Series, i int, want float64) {
	t.Helper()
	got, ok := s.At(i)
	if !ok {
		t.Fatalf("bucket %d is unknown, want %v", i, want)
	}
	if got != want {
		t.Errorf("bucket %d = %v, want %v", i, got, want)
	}
}

// date_bin on the SQL side and AlignDown on the Go side must agree about where a
// bucket starts, or every number lands one bucket out and nothing looks wrong.
func TestTraceCountsLandInTheRightBuckets(t *testing.T) {
	pool := newSourcePool(t)
	summary(t, pool, 0, "ok")
	summary(t, pool, 0, "ok")
	summary(t, pool, 3, "ok")

	got := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "traces", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, got, 0, 2)
	wantBucket(t, got, 3, 1)
	// A count zero-fills: the table records every trace, so an empty bucket
	// genuinely means none started.
	wantBucket(t, got, 1, 0)
	if !got.Filled {
		t.Error("a count series does not record that it was filled")
	}
}

// An average has no value over zero rows, so its empty buckets stay unknown —
// "no traces ran" is not "they averaged nothing".
func TestDurationDoesNotZeroFill(t *testing.T) {
	pool := newSourcePool(t)
	summary(t, pool, 0, "ok", lasting(1000))
	summary(t, pool, 0, "ok", lasting(3000))

	got := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "duration_ns", Aggregate: alerting.AggAvg,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, got, 0, 2000)
	if _, ok := got.At(1); ok {
		t.Error("an empty bucket reported an average")
	}
	if got.Filled {
		t.Error("an average series reported itself as zero-filled")
	}
}

func TestPercentileAndMax(t *testing.T) {
	pool := newSourcePool(t)
	for _, ns := range []int64{100, 200, 300, 400, 100000} {
		summary(t, pool, 0, "ok", lasting(ns))
	}
	p95 := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "duration_ns", Aggregate: alerting.AggP95,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	if v, ok := p95.At(0); !ok || v <= 400 {
		t.Errorf("p95 = (%v, %v), want the tail rather than the body", v, ok)
	}
	max := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "duration_ns", Aggregate: alerting.AggMax,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, max, 0, 100000)
}

// The numerator and the denominator come from one scan, so they cannot disagree
// about which rows they counted.
func TestErrorRateCarriesBothCounts(t *testing.T) {
	pool := newSourcePool(t)
	summary(t, pool, 0, "failed")
	for range 3 {
		summary(t, pool, 0, "ok")
	}

	got := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "error_rate", Aggregate: alerting.AggRatio,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, got, 0, 1) // the numerator
	if den, ok := got.DenominatorAt(0); !ok || den != 4 {
		t.Errorf("denominator = (%v, %v), want (4, true)", den, ok)
	}
	// A bucket with no traces has no rate at all, rather than a zero-percent
	// nobody measured.
	if _, ok := got.At(1); ok {
		t.Error("an empty bucket reported an error rate")
	}
	// And the quotient only exists over a window.
	if v, ok := got.Rolling(alerting.AggRatio, 1, 1).At(0); !ok || v != 0.25 {
		t.Errorf("rolling ratio = (%v, %v), want (0.25, true)", v, ok)
	}
}

// Any status that is not ok is a failure, matching the partial index the UI's
// own "what broke" question is served by.
func TestAnyNonOkStatusCountsAsFailed(t *testing.T) {
	pool := newSourcePool(t)
	summary(t, pool, 0, "failed")
	summary(t, pool, 0, "cancelled")
	summary(t, pool, 0, "ok")

	got := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "failed_traces", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, got, 0, 2)
}

func TestSumsOverTokensAndCost(t *testing.T) {
	pool := newSourcePool(t)
	summary(t, pool, 0, "ok")
	summary(t, pool, 0, "ok")

	tokens := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "tokens", Aggregate: alerting.AggSum,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, tokens, 0, 30) // (10 + 5) twice

	cost := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "cost_usd", Aggregate: alerting.AggSum,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, cost, 0, 0.02)
}

func TestScopeNarrowsTheRows(t *testing.T) {
	pool := newSourcePool(t)
	summary(t, pool, 0, "ok")
	summary(t, pool, 0, "ok", inApp("checkout-worker"))
	summary(t, pool, 0, "ok", inDeployment(depOther))

	byDeployment := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "traces", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, byDeployment, 0, 2)

	byApp := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "traces", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts, AppName: "checkout"},
	})
	wantBucket(t, byApp, 0, 1)

	// The integration axis resolves rows the deployment axis would miss after a
	// rollout, which is the reason it is on the trace table at all.
	byIntegration := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "traces", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{IntegrationID: intAlerts},
	})
	wantBucket(t, byIntegration, 0, 2)
}

func TestLogCountsAndErrorRate(t *testing.T) {
	pool := newSourcePool(t)
	logRow(t, pool, 0, "ERROR", "connection timeout talking to upstream")
	logRow(t, pool, 0, "info", "served a request")
	logRow(t, pool, 0, "info", "served another")
	logRow(t, pool, 2, "fatal", "gave up")

	events := fetch(t, pool, alerting.Query{
		Source: alerting.SourceLogs, Metric: "events", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, events, 0, 3)
	wantBucket(t, events, 1, 0)

	rate := fetch(t, pool, alerting.Query{
		Source: alerting.SourceLogs, Metric: "error_rate", Aggregate: alerting.AggRatio,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	// ERROR and fatal both count, and the casing a logger happened to emit is
	// not a distinction anybody meant to make.
	wantBucket(t, rate, 0, 1)
	if den, _ := rate.DenominatorAt(0); den != 3 {
		t.Errorf("denominator %v, want 3", den)
	}
	if v, _ := rate.At(2); v != 1 {
		t.Errorf("bucket 2 numerator %v, want 1 — fatal is an error", v)
	}
}

func TestLogLevelAndSearchFilters(t *testing.T) {
	pool := newSourcePool(t)
	logRow(t, pool, 0, "error", "connection timeout talking to upstream")
	logRow(t, pool, 0, "error", "permission denied")
	logRow(t, pool, 0, "info", "connection timeout, retrying")

	byLevel := fetch(t, pool, alerting.Query{
		Source: alerting.SourceLogs, Metric: "events", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts, Levels: []string{"ERROR"}},
	})
	wantBucket(t, byLevel, 0, 2)

	bySearch := fetch(t, pool, alerting.Query{
		Source: alerting.SourceLogs, Metric: "events", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts, Levels: []string{"error"}, Search: "timeout"},
	})
	wantBucket(t, bySearch, 0, 1)
}

// The range is half-open on both sides of the boundary: a row exactly at `to`
// belongs to the next window, not this one.
func TestTheWindowIsHalfOpen(t *testing.T) {
	pool := newSourcePool(t)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO trace_summaries (trace_id, deployment_id, app_name, app_version, started_at, ended_at, status)
		VALUES (gen_random_uuid()::text, $1::uuid, 'checkout', 'v1', $2, $2, 'ok')`,
		depAlerts, seedStart.Add(fetchWindow*time.Minute))
	if err != nil {
		t.Fatalf("seed the boundary row: %v", err)
	}
	summary(t, pool, 0, "ok")

	got := fetch(t, pool, alerting.Query{
		Source: alerting.SourceTraces, Metric: "traces", Aggregate: alerting.AggCount,
		Scope: alerting.Scope{DeploymentID: depAlerts},
	})
	wantBucket(t, got, 0, 1)
	wantBucket(t, got, fetchWindow-1, 0)
}
