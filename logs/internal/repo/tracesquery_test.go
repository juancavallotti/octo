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

// listAll pages through every trace matching f, following the cursor exactly as a
// client would, and returns the ids in the order they were served.
func listAll(t *testing.T, store *Traces, f TraceFilter) []string {
	t.Helper()
	ctx := context.Background()

	var served []string
	for page := 0; page < 50; page++ {
		rows, err := store.List(ctx, f)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, row := range rows {
			served = append(served, row.TraceID)
		}
		if len(rows) < f.Limit {
			return served
		}
		last := rows[len(rows)-1]
		f.Before = &TraceCursor{StartedAt: last.StartedAt, TraceID: last.TraceID}
	}
	t.Fatal("pagination did not terminate")
	return nil
}

// TestTraceListPagesThroughTiesExactlyOnce is the reason the cursor is a pair.
//
// Every trace here starts in the same microsecond, which is what a burst of
// requests actually looks like. A cursor on the timestamp alone would either skip
// the rows that tie across a page boundary or serve them again on the next page,
// and both failures are invisible in a page-sized sample.
func TestTraceListPagesThroughTiesExactlyOnce(t *testing.T) {
	store := newTestTraceApps(t)

	const total = 7
	rows := make([]ingest.TraceRow, 0, total)
	want := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		id := "apps-tie-" + string(rune('a'+i))
		// Identical timestamps and durations, so started_at ties exactly rather
		// than merely closely.
		rows = append(rows, appRecord(id, int64(i), ingest.KindFlowCompleted, 5))
		want[id] = true
	}
	if err := store.Insert(context.Background(), rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// A page size that does not divide the total, so a boundary lands mid-tie.
	served := listAll(t, store, TraceFilter{DeploymentID: depApps, Limit: 2})

	seen := map[string]int{}
	for _, id := range served {
		seen[id]++
	}
	for id := range want {
		switch seen[id] {
		case 1:
		case 0:
			t.Errorf("trace %s was skipped across a page boundary", id)
		default:
			t.Errorf("trace %s was served %d times", id, seen[id])
		}
	}
	if len(served) != total {
		t.Errorf("served %d rows for %d traces: %v", len(served), total, served)
	}
}

// TestTraceListOrdersNewestFirstAcrossPages checks paging preserves the ordering
// rather than merely covering every row.
func TestTraceListOrdersNewestFirstAcrossPages(t *testing.T) {
	store := newTestTraceApps(t)

	var rows []ingest.TraceRow
	for i := 0; i < 6; i++ {
		rows = append(rows, appRecord("apps-seq-"+string(rune('a'+i)), int64(i), ingest.KindFlowCompleted, i))
	}
	if err := store.Insert(context.Background(), rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	served := listAll(t, store, TraceFilter{DeploymentID: depApps, Limit: 2})
	want := []string{"apps-seq-f", "apps-seq-e", "apps-seq-d", "apps-seq-c", "apps-seq-b", "apps-seq-a"}
	if len(served) != len(want) {
		t.Fatalf("served %v, want %v", served, want)
	}
	for i := range want {
		if served[i] != want[i] {
			t.Fatalf("served %v, want %v", served, want)
		}
	}
}

// TestTraceListFilters exercises each axis against the database, since a filter
// that compiles can still select the wrong rows.
func TestTraceListFilters(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	slow := appRecord("apps-f-slow", 1, ingest.KindFlowCompleted, 10)
	slow.Record.DurationNs = int64(900 * time.Millisecond)

	llm := appRecord("apps-f-llm", 2, ingest.KindLLMTurn, 11)
	llm.Record.Model = "gpt-4o"
	llm.Record.Usage = &cost.Usage{InputTokens: 5, OutputTokens: 5}

	// A flow of its own, so the flow filter has something to discriminate on.
	// Every other record here runs in "orders", which would let a flow filter that
	// was never applied still return the expected row.
	elsewhere := appRecord("apps-f-flow", 5, ingest.KindFlowCompleted, 15)
	elsewhere.Record.Flow = "refunds"

	// A route that appears in no flow name and no trace id, so a search matching
	// it can only have matched the entry label.
	routed := appRecord("apps-f-routed", 6, ingest.KindSourceReceive, 16)
	routed.Record.Flow = ""
	routed.Record.Attrs = json.RawMessage(`{"method":"PUT","route":"/wishlist"}`)

	err := store.Insert(ctx, []ingest.TraceRow{
		appRecord("apps-f-plain", 0, ingest.KindFlowCompleted, 12),
		slow,
		llm,
		appRecord("apps-f-failed", 3, ingest.KindFlowFailed, 13),
		appRecord("apps-f-v2", 4, ingest.KindFlowCompleted, 14, atVersion("v2")),
		elsewhere,
		routed,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	cases := []struct {
		name   string
		filter TraceFilter
		want   []string
	}{
		{"status", TraceFilter{Statuses: []string{StatusFailed}}, []string{"apps-f-failed"}},
		{"version", TraceFilter{AppVersion: "v2"}, []string{"apps-f-v2"}},
		{"has llm", TraceFilter{HasLLM: true}, []string{"apps-f-llm"}},
		{"min duration", TraceFilter{MinDuration: 500 * time.Millisecond}, []string{"apps-f-slow"}},
		{"flow", TraceFilter{Flow: "refunds"}, []string{"apps-f-flow"}},
		{"search by trace id", TraceFilter{Search: "f-slow"}, []string{"apps-f-slow"}},
		{"search by entry label", TraceFilter{Search: "wishlist"}, []string{"apps-f-routed"}},
		{"unfiltered", TraceFilter{}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.filter
			f.DeploymentID = depApps
			f.Limit = 50
			rows, err := store.List(ctx, f)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if tc.want == nil {
				if len(rows) != 7 {
					t.Fatalf("matched %d traces (%v), want all 7", len(rows), traceIDs(rows))
				}
				return
			}
			if len(rows) != len(tc.want) {
				t.Fatalf("matched %d traces (%v), want %v", len(rows), traceIDs(rows), tc.want)
			}
			for i, id := range tc.want {
				if rows[i].TraceID != id {
					t.Errorf("row %d = %s, want %s", i, rows[i].TraceID, id)
				}
			}
		})
	}
}

func traceIDs(rows []TraceListRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.TraceID)
	}
	return out
}

// TestTraceListReportsWhatARowIsFor checks the projection carries the columns the
// list renders, including the ones only the rollup could know.
func TestTraceListReportsWhatARowIsFor(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	entry := appRecord("apps-row", 1, ingest.KindSourceReceive, 3)
	entry.Record.Flow = ""
	entry.Record.Attrs = json.RawMessage(`{"method":"POST","route":"/orders"}`)

	call := appRecord("apps-row", 2, ingest.KindLLMTurn, 4)
	call.Record.Model = "gpt-4o"
	call.Record.Usage = &cost.Usage{InputTokens: 30, OutputTokens: 12, CachedTokens: 5}
	call.Priced = cost.Priced{Status: cost.StatusUnpricedModel}

	done := appRecord("apps-row", 3, ingest.KindFlowCompleted, 5)
	done.Record.DurationNs = int64(250 * time.Millisecond)

	if err := store.Insert(ctx, []ingest.TraceRow{entry, call, done}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := store.List(ctx, TraceFilter{DeploymentID: depApps, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("matched %d traces, want 1", len(rows))
	}

	row := rows[0]
	if row.EntryKind != ingest.KindSourceReceive || row.EntryLabel != "POST /orders" {
		t.Errorf("entry = %q/%q, want source.receive/POST /orders", row.EntryKind, row.EntryLabel)
	}
	if row.RootFlow != "orders" {
		t.Errorf("root flow = %q, want orders", row.RootFlow)
	}
	if row.Records != 3 || row.LLMCalls != 1 {
		t.Errorf("records/llm = %d/%d, want 3/1", row.Records, row.LLMCalls)
	}
	if row.CostUSD != 0 || row.UnpricedCalls != 1 {
		t.Errorf("cost = %v with %d unpriced, want 0 with 1", row.CostUSD, row.UnpricedCalls)
	}
	if len(row.Models) != 1 || row.Models[0] != "gpt-4o" {
		t.Errorf("models = %v, want [gpt-4o]", row.Models)
	}
	if len(row.DeploymentIDs) != 1 || row.DeploymentIDs[0] != depApps {
		t.Errorf("deployments = %v, want [%s]", row.DeploymentIDs, depApps)
	}
	if row.RootDurationNs != int64(250*time.Millisecond) {
		t.Errorf("root duration = %d, want %d", row.RootDurationNs, int64(250*time.Millisecond))
	}
}

// TestTraceListScopesToOneAppAndWindow checks the two filters a caller never sets
// by hand: the view opens on one app over one window, so a query that ignored
// either would fill a checkout timeline with another app's traces, or with
// traces from last week.
func TestTraceListScopesToOneAppAndWindow(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	other := appRecord("apps-scope-other", 1, ingest.KindFlowCompleted, 5)
	other.Record.DeploymentID = depAppsOther

	err := store.Insert(ctx, []ingest.TraceRow{
		appRecord("apps-scope-mine", 2, ingest.KindFlowCompleted, 5),
		appRecord("apps-scope-old", 3, ingest.KindFlowCompleted, -120),
		appRecord("apps-scope-new", 4, ingest.KindFlowCompleted, 600),
		other,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	from, to := appsAt(0), appsAt(60)
	rows, err := store.List(ctx, TraceFilter{DeploymentID: depApps, From: &from, To: &to, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].TraceID != "apps-scope-mine" {
		t.Fatalf("matched %v, want only apps-scope-mine", traceIDs(rows))
	}
}

// TestTraceListScopesToOneIntegration checks the axis that spans deployments. An
// integration outlives the deployments that serve it, so "every trace this
// integration has ever produced" is a different question from "every trace this
// deployment produced" — and answering the first with the second's filter would
// quietly return one release's worth.
func TestTraceListScopesToOneIntegration(t *testing.T) {
	store := newTestTraceApps(t)
	ctx := context.Background()

	// Same integration, a second deployment: what a redeploy leaves behind.
	redeployed := appRecord("apps-int-second", 1, ingest.KindFlowCompleted, 6)
	redeployed.Record.DeploymentID = depAppsOther

	// A different integration entirely, which must not appear.
	stranger := appRecord("apps-int-stranger", 2, ingest.KindFlowCompleted, 7)
	stranger.Record.DeploymentID = depAppsOther
	stranger.IntegrationID = intBack

	err := store.Insert(ctx, []ingest.TraceRow{
		appRecord("apps-int-first", 3, ingest.KindFlowCompleted, 5),
		redeployed,
		stranger,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := store.List(ctx, TraceFilter{IntegrationID: intApps, Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	got := traceIDs(rows)
	if len(got) != 2 {
		t.Fatalf("matched %v, want both of this integration's traces and neither of the other's", got)
	}
	for _, id := range got {
		if id == "apps-int-stranger" {
			t.Errorf("matched %v, which includes another integration's trace", got)
		}
	}
}
