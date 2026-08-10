package repo

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/logs/internal/cost"
	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// The app-list query aggregates across every deployment in a window rather than
// filtering to one, so these tests carry deployments and a window of their own.
// Sharing either with the store tests would make each suite's rows show up in the
// other's answers.
const (
	depApps      = "55555555-5555-5555-5555-555555555555"
	depAppsOther = "66666666-6666-6666-6666-666666666666"
	intApps      = "77777777-7777-7777-7777-777777777777"
)

// appsWindow is far from traceStart so no other test's rows fall inside it.
var appsWindow = time.Date(2031, 3, 4, 9, 0, 0, 0, time.UTC)

func appsAt(minutes int) time.Time { return appsWindow.Add(time.Duration(minutes) * time.Minute) }

// newTestTraceApps opens a pool and cleans up both deployments this suite writes
// under, including the rows carrying no trace id.
func newTestTraceApps(t *testing.T) *Traces {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run trace query tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, deployment := range []string{depApps, depAppsOther} {
			_, _ = pool.Exec(ctx, `DELETE FROM traces WHERE deployment_id = $1::uuid`, deployment)
			_, _ = pool.Exec(ctx, `DELETE FROM trace_summaries WHERE deployment_id = $1::uuid`, deployment)
		}
		pool.Close()
	})
	return NewTraces(pool)
}

// appRecord builds a record under this suite's deployment and window.
func appRecord(traceID string, seq int64, kind string, minute int, opts ...func(*ingest.TraceRow)) ingest.TraceRow {
	row := ingest.TraceRow{
		Record: ingest.TraceRecord{
			TraceID:      traceID,
			Seq:          seq,
			Kind:         kind,
			Flow:         "orders",
			DeploymentID: depApps,
			AppName:      "checkout",
			AppVersion:   "v1",
			Time:         appsAt(minute),
		},
		IntegrationID: intApps,
	}
	for _, opt := range opts {
		opt(&row)
	}
	return row
}

func atVersion(version string) func(*ingest.TraceRow) {
	return func(r *ingest.TraceRow) { r.Record.AppVersion = version }
}

// findApp returns the row for one app, so an assertion names what it is about
// rather than an index.
func findApp(t *testing.T, apps []TraceApp, deployment, version string) TraceApp {
	t.Helper()
	for _, app := range apps {
		if app.DeploymentID == deployment && app.AppVersion == version {
			return app
		}
	}
	t.Fatalf("no row for deployment %s at version %s in %+v", deployment, version, apps)
	return TraceApp{}
}

// TestTraceAppsCountsOneRowPerVersion checks a rollout shows as two rows rather
// than one. The deployment id does not change across a rollout, so grouping by it
// alone would fold a new version's traces into the old version's totals — and a
// cost that belongs to one release would be reported against both.
func TestTraceAppsCountsOneRowPerVersion(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	err := store.Insert(ctx, []ingest.TraceRow{
		appRecord("apps-v1-a", 1, ingest.KindFlowCompleted, 1),
		appRecord("apps-v1-b", 2, ingest.KindFlowFailed, 2),
		appRecord("apps-v2-a", 3, ingest.KindFlowCompleted, 3, atVersion("v2")),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	apps, err := store.Apps(ctx, appsAt(0), appsAt(60))
	if err != nil {
		t.Fatalf("apps: %v", err)
	}

	v1 := findApp(t, apps, depApps, "v1")
	if v1.Traces != 2 || v1.Failed != 1 {
		t.Errorf("v1 = %d traces / %d failed, want 2/1", v1.Traces, v1.Failed)
	}
	v2 := findApp(t, apps, depApps, "v2")
	if v2.Traces != 1 || v2.Failed != 0 {
		t.Errorf("v2 = %d traces / %d failed, want 1/0", v2.Traces, v2.Failed)
	}
	if v1.IntegrationID != intApps {
		t.Errorf("integration = %q, want %q", v1.IntegrationID, intApps)
	}
}

// TestTraceAppsHonoursItsWindow checks the bounds actually bound. A count is only
// meaningful against the window it was measured over, so a query that quietly
// included traces outside it would be reporting a different number than the one
// the caller asked for.
func TestTraceAppsHonoursItsWindow(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	err := store.Insert(ctx, []ingest.TraceRow{
		appRecord("apps-early", 1, ingest.KindFlowCompleted, -30),
		appRecord("apps-inside", 2, ingest.KindFlowCompleted, 10),
		appRecord("apps-late", 3, ingest.KindFlowCompleted, 90),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	apps, err := store.Apps(ctx, appsAt(0), appsAt(60))
	if err != nil {
		t.Fatalf("apps: %v", err)
	}
	if got := findApp(t, apps, depApps, "v1").Traces; got != 1 {
		t.Errorf("traces = %d, want only the one inside the window", got)
	}
}

// TestTraceAppsSumsCostApartFromWhatCouldNotBePriced checks the two travel
// together out of the aggregate. Summing an unpriced call into the total as zero
// is the failure this whole path is built to prevent, and an aggregate is where
// it would finally look like a confident number.
func TestTraceAppsSumsCostApartFromWhatCouldNotBePriced(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	priced := func(traceID string, seq int64, minute int, status cost.Status) ingest.TraceRow {
		row := appRecord(traceID, seq, ingest.KindLLMTurn, minute)
		row.Record.Model = "gpt-4o"
		row.Record.Usage = &cost.Usage{InputTokens: 10, OutputTokens: 5}
		row.Priced = cost.Priced{Status: status}
		return row
	}

	// A rate card the test controls, so the expected total is arithmetic rather
	// than whatever the live catalogue says today.
	card := cost.NewTable([]cost.Rate{{
		ID: rateID, Provider: "OPENAI", Pattern: "gpt-4o", Operator: cost.OpEquals,
		InputPer1M: 1_000_000, OutputPer1M: 2_000_000,
	}})
	billed := priced("apps-cost-a", 1, 1, cost.StatusPriced)
	billed.Priced = card.Price(cost.Call{Model: "gpt-4o", Usage: billed.Record.Usage})

	err := store.Insert(ctx, []ingest.TraceRow{
		billed,
		priced("apps-cost-b", 2, 2, cost.StatusUnpricedModel),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	apps, err := store.Apps(ctx, appsAt(0), appsAt(60))
	if err != nil {
		t.Fatalf("apps: %v", err)
	}

	app := findApp(t, apps, depApps, "v1")
	if want := 10.0 + 10.0; app.CostUSD != want {
		t.Errorf("cost = %v, want %v (only the call that could be priced)", app.CostUSD, want)
	}
	if app.UnpricedCalls != 1 {
		t.Errorf("unpriced calls = %d, want 1", app.UnpricedCalls)
	}
}

// TestTraceAppsReportsAnAppWithNothingButDrops is why the two halves are joined
// FULL OUTER. An app whose records were all shed has no summaries at all, and
// that is precisely when a reader needs to be told something was lost — reporting
// it as no activity would hide the loss behind an absence.
func TestTraceAppsReportsAnAppWithNothingButDrops(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	marker := ingest.TraceRow{
		Record: ingest.TraceRecord{
			Seq:          9,
			Kind:         ingest.KindTraceDropped,
			DeploymentID: depAppsOther,
			AppName:      "shedding",
			AppVersion:   "v4",
			Time:         appsAt(5),
			Attrs:        json.RawMessage(`{"dropped":12,"total":900}`),
		},
		IntegrationID: intApps,
	}
	if err := store.Insert(ctx, []ingest.TraceRow{marker}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	apps, err := store.Apps(ctx, appsAt(0), appsAt(60))
	if err != nil {
		t.Fatalf("apps: %v", err)
	}

	app := findApp(t, apps, depAppsOther, "v4")
	if app.DroppedRecords != 12 {
		t.Errorf("dropped = %d, want 12", app.DroppedRecords)
	}
	if app.Traces != 0 {
		t.Errorf("traces = %d, want 0 — the marker belongs to no trace", app.Traces)
	}
	if !app.LastSeenAt.Equal(appsAt(5)) {
		t.Errorf("last seen = %s, want the marker's own timestamp %s", app.LastSeenAt, appsAt(5))
	}
}

// TestTraceAppsAttributesDropsToTheAppThatLostThem checks a marker lands on its
// own app's row rather than being counted against every app in the window.
func TestTraceAppsAttributesDropsToTheAppThatLostThem(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	marker := ingest.TraceRow{
		Record: ingest.TraceRecord{
			Seq: 9, Kind: ingest.KindTraceDropped,
			DeploymentID: depApps, AppName: "checkout", AppVersion: "v1",
			Time:  appsAt(4),
			Attrs: json.RawMessage(`{"dropped":7,"total":100}`),
		},
		IntegrationID: intApps,
	}
	err := store.Insert(ctx, []ingest.TraceRow{
		appRecord("apps-drop-a", 1, ingest.KindFlowCompleted, 1),
		appRecord("apps-drop-b", 2, ingest.KindFlowCompleted, 2, atVersion("v2")),
		marker,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	apps, err := store.Apps(ctx, appsAt(0), appsAt(60))
	if err != nil {
		t.Fatalf("apps: %v", err)
	}

	if got := findApp(t, apps, depApps, "v1"); got.DroppedRecords != 7 || got.Traces != 1 {
		t.Errorf("v1 = %d dropped / %d traces, want 7/1", got.DroppedRecords, got.Traces)
	}
	if got := findApp(t, apps, depApps, "v2").DroppedRecords; got != 0 {
		t.Errorf("v2 dropped = %d, want 0 — the marker was the other version's", got)
	}
}

// TestTraceAppsOrdersByMostRecentlyActive pins the order the app list is read in.
func TestTraceAppsOrdersByMostRecentlyActive(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	stale := appRecord("apps-order-a", 1, ingest.KindFlowCompleted, 2)
	fresh := appRecord("apps-order-b", 2, ingest.KindFlowCompleted, 40, atVersion("v2"))
	if err := store.Insert(ctx, []ingest.TraceRow{stale, fresh}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	apps, err := store.Apps(ctx, appsAt(0), appsAt(60))
	if err != nil {
		t.Fatalf("apps: %v", err)
	}
	if len(apps) < 2 {
		t.Fatalf("apps = %+v, want both versions", apps)
	}
	if apps[0].AppVersion != "v2" {
		t.Errorf("first row = %q, want the most recently active (v2)", apps[0].AppVersion)
	}
}
