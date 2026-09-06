package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/observability/internal/ingest"
)

// This suite writes under a deployment of its own so its rows cannot show up in
// another suite's answers — and, more to the point here, so that a sweep it runs
// cannot delete another suite's rows. It also works in a window far from any
// other's, because a prune is defined by a cutoff rather than by a filter.
const (
	depPrune = "99999999-9999-9999-9999-999999999999"
	intPrune = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

var pruneNow = time.Date(2033, 3, 1, 12, 0, 0, 0, time.UTC)

// hoursFromCutoff returns a time offset from the suite's cutoff; negative is
// expired, positive is inside the retention window.
func hoursFromCutoff(h int) time.Time { return pruneNow.Add(time.Duration(h) * time.Hour) }

func newPrunePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run prune tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		// Scoped by deployment rather than by trace id: a dropped-records marker
		// belongs to no trace, so cleaning up by trace id would leave it behind.
		_, _ = pool.Exec(ctx, `DELETE FROM logs WHERE deployment_id = $1::uuid`, depPrune)
		_, _ = pool.Exec(ctx, `DELETE FROM traces WHERE deployment_id = $1::uuid`, depPrune)
		_, _ = pool.Exec(ctx, `DELETE FROM trace_summaries WHERE deployment_id = $1::uuid`, depPrune)
		pool.Close()
	})
	return pool
}

// insertLogAt stores one log event stamped at ts.
func insertLogAt(t *testing.T, store *Logs, ts time.Time, message string) {
	t.Helper()
	err := store.Insert(context.Background(), ingest.LogEvent{
		DeploymentID: depPrune,
		AppName:      "storefront",
		AppVersion:   "v9",
		Time:         ts,
		Level:        "INFO",
		Message:      message,
	})
	if err != nil {
		t.Fatalf("insert log %q: %v", message, err)
	}
}

// survivingLogs returns the messages this suite's deployment still has stored.
func survivingLogs(t *testing.T, store *Logs) []string {
	t.Helper()
	rows, err := store.Query(context.Background(), LogFilter{DeploymentID: depPrune, Limit: 100})
	if err != nil {
		t.Fatalf("query logs: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Message)
	}
	return out
}

func TestLogsPruneDeletesOnlyWhatExpired(t *testing.T) {
	store := NewLogs(newPrunePool(t))

	insertLogAt(t, store, hoursFromCutoff(-48), "two days old")
	insertLogAt(t, store, hoursFromCutoff(-1), "an hour old")
	insertLogAt(t, store, hoursFromCutoff(1), "an hour from now")

	deleted, err := store.Prune(context.Background(), pruneNow, 100)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d rows, want 2", deleted)
	}

	got := survivingLogs(t, store)
	if len(got) != 1 || got[0] != "an hour from now" {
		t.Fatalf("survivors = %v, want just the row inside the window", got)
	}
}

// The batch size is not a cap on the sweep. A first run after the policy is
// switched on can have far more to delete than one statement should take at
// once, and it still has to finish.
func TestLogsPruneLoopsPastTheBatchSize(t *testing.T) {
	store := NewLogs(newPrunePool(t))

	const rows = 7
	for i := range rows {
		insertLogAt(t, store, hoursFromCutoff(-24-i), "expired")
	}
	insertLogAt(t, store, hoursFromCutoff(1), "kept")

	deleted, err := store.Prune(context.Background(), pruneNow, 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != rows {
		t.Fatalf("deleted %d rows, want %d", deleted, rows)
	}
	if got := survivingLogs(t, store); len(got) != 1 {
		t.Fatalf("survivors = %v, want only the row inside the window", got)
	}
}

// pruneRecord builds a trace record under this suite's deployment.
func pruneRecord(traceID string, seq int64, kind string, end time.Time, durationMs int) ingest.TraceRow {
	return ingest.TraceRow{
		Record: ingest.TraceRecord{
			TraceID:      traceID,
			Seq:          seq,
			Kind:         kind,
			DeploymentID: depPrune,
			AppName:      "storefront",
			AppVersion:   "v9",
			Flow:         "orders",
			Time:         end,
			DurationNs:   ms(durationMs),
		},
		IntegrationID: intPrune,
	}
}

// countTraceRows reports how many records and summaries this suite still has.
func countTraceRows(t *testing.T, store *Traces) (records, summaries int) {
	t.Helper()
	ctx := context.Background()
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FROM traces WHERE deployment_id = $1::uuid`, depPrune,
	).Scan(&records); err != nil {
		t.Fatalf("count records: %v", err)
	}
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FROM trace_summaries WHERE deployment_id = $1::uuid`, depPrune,
	).Scan(&summaries); err != nil {
		t.Fatalf("count summaries: %v", err)
	}
	return records, summaries
}

// A trace is deleted whole or not at all. This one starts before the cutoff and
// is still emitting records after it — sweeping the two tables on their own
// timestamps would take the summary and leave the late records behind, and a
// trace with records but no summary is one the detail route cannot serve.
func TestTracesPruneTakesAStraddlingTraceWhole(t *testing.T) {
	store := NewTraces(newPrunePool(t))
	ctx := context.Background()

	rows := []ingest.TraceRow{
		pruneRecord("prune-straddle", 1, ingest.KindFlowStarted, hoursFromCutoff(-5), 0),
		pruneRecord("prune-straddle", 2, ingest.KindBlockPostInvoke, hoursFromCutoff(-4), 10),
		pruneRecord("prune-straddle", 3, ingest.KindFlowCompleted, hoursFromCutoff(3), 10),
	}
	if err := store.Insert(ctx, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	result, err := store.Prune(ctx, pruneNow, 100)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.Records != 3 || result.Summaries != 1 {
		t.Fatalf("result = %+v, want 3 records and 1 summary", result)
	}

	records, summaries := countTraceRows(t, store)
	if records != 0 || summaries != 0 {
		t.Fatalf("left %d records and %d summaries behind", records, summaries)
	}
}

func TestTracesPruneKeepsATraceInsideTheWindow(t *testing.T) {
	store := NewTraces(newPrunePool(t))
	ctx := context.Background()

	rows := []ingest.TraceRow{
		pruneRecord("prune-recent", 1, ingest.KindFlowStarted, hoursFromCutoff(2), 0),
		pruneRecord("prune-recent", 2, ingest.KindFlowCompleted, hoursFromCutoff(3), 10),
	}
	if err := store.Insert(ctx, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	result, err := store.Prune(ctx, pruneNow, 100)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.Records != 0 || result.Summaries != 0 {
		t.Fatalf("result = %+v, want nothing deleted", result)
	}

	records, summaries := countTraceRows(t, store)
	if records != 2 || summaries != 1 {
		t.Fatalf("have %d records and %d summaries, want 2 and 1", records, summaries)
	}
}

// A dropped-records marker carries no trace id, so it never produces a summary
// and the by-trace pass can never reach it. Without the orphan pass these
// accumulate for the life of the installation.
func TestTracesPruneCollectsDroppedMarkers(t *testing.T) {
	store := NewTraces(newPrunePool(t))
	ctx := context.Background()

	rows := []ingest.TraceRow{
		pruneRecord("", 1, ingest.KindTraceDropped, hoursFromCutoff(-6), 0),
		pruneRecord("", 2, ingest.KindTraceDropped, hoursFromCutoff(4), 0),
	}
	if err := store.Insert(ctx, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	records, summaries := countTraceRows(t, store)
	if records != 2 || summaries != 0 {
		t.Fatalf("markers stored as %d records / %d summaries, want 2 and 0", records, summaries)
	}

	result, err := store.Prune(ctx, pruneNow, 100)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.Records != 1 || result.Summaries != 0 {
		t.Fatalf("result = %+v, want the expired marker and nothing else", result)
	}

	records, _ = countTraceRows(t, store)
	if records != 1 {
		t.Fatalf("%d markers left, want the one inside the window", records)
	}
}

// The orphan pass loops too, and it must not take rows inside the window with it.
func TestTracesPruneLoopsPastTheBatchSize(t *testing.T) {
	store := NewTraces(newPrunePool(t))
	ctx := context.Background()

	var rows []ingest.TraceRow
	for i := range 5 {
		rows = append(rows,
			pruneRecord("prune-batch-"+string(rune('a'+i)), 1, ingest.KindFlowStarted, hoursFromCutoff(-10-i), 0))
		rows = append(rows, pruneRecord("", int64(i), ingest.KindTraceDropped, hoursFromCutoff(-10-i), 0))
	}
	rows = append(rows, pruneRecord("prune-batch-kept", 1, ingest.KindFlowStarted, hoursFromCutoff(6), 0))
	if err := store.Insert(ctx, rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	result, err := store.Prune(ctx, pruneNow, 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.Records != 10 || result.Summaries != 5 {
		t.Fatalf("result = %+v, want 10 records and 5 summaries", result)
	}

	records, summaries := countTraceRows(t, store)
	if records != 1 || summaries != 1 {
		t.Fatalf("have %d records and %d summaries, want only the trace inside the window", records, summaries)
	}
}
