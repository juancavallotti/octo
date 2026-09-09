package snapshot

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool opens a pool against TEST_DATABASE_URL, skipping the test when it
// is unset so `go test ./...` stays green without a database.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run integration repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestCreateFreezesResources verifies that tagging an integration copies its
// files into integration_file_snapshots and that later edits to the live file do
// not affect the frozen copy.
func TestCreateFreezesResources(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewRepo(pool)

	var intID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO integrations (name) VALUES ($1) RETURNING id`, "freeze-test",
	).Scan(&intID); err != nil {
		t.Fatalf("seed integration: %v", err)
	}
	// Deleting the integration cascades to its resources, snapshots and frozen
	// resources, so this one cleanup covers everything seeded below.
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM integrations WHERE id = $1`, intID) })

	if _, err := pool.Exec(ctx,
		`INSERT INTO integration_files (integration_id, kind, path, content, role)
		 VALUES ($1, $2, $3, $4, $5)`,
		intID, "env", ".env.dev", "GREETING=hi", "resource",
	); err != nil {
		t.Fatalf("seed resource: %v", err)
	}

	snap, err := repo.Create(ctx, intID, "v1")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	frozen := func() string {
		var content string
		if err := pool.QueryRow(ctx,
			`SELECT content FROM integration_file_snapshots WHERE snapshot_id = $1 AND path = $2`,
			snap.ID, ".env.dev",
		).Scan(&content); err != nil {
			t.Fatalf("read frozen resource: %v", err)
		}
		return content
	}

	if got := frozen(); got != "GREETING=hi" {
		t.Errorf("frozen content = %q, want the live content at tag time", got)
	}

	// Editing the live resource must not disturb the frozen copy.
	if _, err := pool.Exec(ctx,
		`UPDATE integration_files SET content = $1 WHERE integration_id = $2 AND path = $3`,
		"GREETING=changed", intID, ".env.dev",
	); err != nil {
		t.Fatalf("edit live resource: %v", err)
	}
	if got := frozen(); got != "GREETING=hi" {
		t.Errorf("frozen content changed to %q after a live edit; it must stay frozen", got)
	}
}
