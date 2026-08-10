package repo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// This suite writes under a deployment of its own so its rows cannot show up in
// another suite's answers, and in a window far from any other's.
const depLogs = "88888888-8888-8888-8888-888888888888"

var logsWindow = time.Date(2032, 5, 6, 11, 0, 0, 0, time.UTC)

// newTestLogs opens a pool and cleans up the rows this suite writes.
func newTestLogs(t *testing.T) *Logs {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run log query tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM logs WHERE deployment_id = $1::uuid`, depLogs)
		pool.Close()
	})
	return NewLogs(pool)
}

// pageAll pages through every log matching f, following the cursor exactly as a
// client would, and returns the ids in the order they were served.
func pageAll(t *testing.T, store *Logs, f LogFilter) []string {
	t.Helper()
	ctx := context.Background()

	var served []string
	for page := 0; page < 50; page++ {
		rows, err := store.Query(ctx, f)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		for _, row := range rows {
			served = append(served, row.ID)
		}
		if len(rows) < f.Limit {
			return served
		}
		last := rows[len(rows)-1]
		f.Before = &LogCursor{Time: last.Time, ID: last.ID}
	}
	t.Fatal("pagination did not terminate")
	return nil
}

// TestLogsPageThroughTiesExactlyOnce is the reason the cursor is a pair.
//
// Every record here carries the same timestamp, which is what a burst of log
// lines from one request actually looks like — the runtime stamps them from the
// same clock read, and Postgres stores microseconds. A cursor on the timestamp
// alone either skips the rows that tie across a page boundary or serves them
// again on the next page, and both failures are invisible in a page-sized
// sample: you only see them by paging the whole set and counting.
func TestLogsPageThroughTiesExactlyOnce(t *testing.T) {
	store := newTestLogs(t)
	ctx := context.Background()

	const total = 7
	want := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		if err := store.Insert(ctx, ingest.LogEvent{
			DeploymentID: depLogs,
			AppName:      "svc",
			AppVersion:   "v1",
			Time:         logsWindow, // identical, so ts ties exactly rather than closely
			Level:        "INFO",
			Message:      "line",
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// Ids are assigned by the database, so read them back rather than assuming.
	rows, err := store.Query(ctx, LogFilter{DeploymentID: depLogs, Limit: total + 1})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(rows) != total {
		t.Fatalf("inserted %d rows, read back %d", total, len(rows))
	}
	for _, row := range rows {
		want[row.ID] = true
	}

	// A page size that does not divide the total, so a boundary lands inside the
	// tied group more than once.
	served := pageAll(t, store, LogFilter{DeploymentID: depLogs, Limit: 2})

	if len(served) != total {
		t.Errorf("served %d rows across all pages, want %d: %v", len(served), total, served)
	}
	seen := make(map[string]int, total)
	for _, id := range served {
		seen[id]++
	}
	for id := range want {
		switch seen[id] {
		case 1:
		case 0:
			t.Errorf("row %s was skipped at a page boundary", id)
		default:
			t.Errorf("row %s was served %d times", id, seen[id])
		}
	}
}

// TestLogsPageInDescendingOrder pins the ordering the cursor depends on: newest
// first, ties broken by id descending. A page that came back in another order
// would still page correctly by accident here, so it is asserted separately.
func TestLogsPageInDescendingOrder(t *testing.T) {
	store := newTestLogs(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := store.Insert(ctx, ingest.LogEvent{
			DeploymentID: depLogs,
			Time:         logsWindow.Add(time.Duration(i) * time.Minute),
			Level:        "INFO",
			Message:      "line",
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	rows, err := store.Query(ctx, LogFilter{DeploymentID: depLogs, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if cur.Time.After(prev.Time) {
			t.Fatalf("row %d (%v) is newer than row %d (%v): not newest-first",
				i, cur.Time, i-1, prev.Time)
		}
		if cur.Time.Equal(prev.Time) && cur.ID >= prev.ID {
			t.Fatalf("rows %d and %d tie on ts but ids are not descending: %s then %s",
				i-1, i, prev.ID, cur.ID)
		}
	}
}
