package resource

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool opens a pool against TEST_DATABASE_URL, skipping when it is unset
// so `go test ./...` stays green without a database.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run resource repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newIntegration inserts a bare integration to hang resources off, and cleans it
// up (the cascade takes the files with it).
func newIntegration(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	ctx := context.Background()

	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO integrations (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("create integration: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM integrations WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup integration %s: %v", id, err)
		}
	})
	return id
}

func TestRepoResourceRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()
	integrationID := newIntegration(t, pool, "resource-round-trip")

	created, err := r.Create(ctx, integrationID, KindEnv, ".env.dev", "TOKEN=abc", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != ".env.dev" || created.Kind != KindEnv || created.Content != "TOKEN=abc" {
		t.Fatalf("created = %+v", created)
	}

	got, err := r.Get(ctx, integrationID, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != created.Name || got.Content != created.Content {
		t.Errorf("get = %+v, want %+v", got, created)
	}

	updated, err := r.Update(ctx, integrationID, created.ID, KindEnv, ".env.dev", "TOKEN=xyz", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Content != "TOKEN=xyz" {
		t.Errorf("update content = %q", updated.Content)
	}

	if err := r.Delete(ctx, integrationID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.Get(ctx, integrationID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
}

// Flow files and resources share a table now, so the thing worth proving is that
// the resource API cannot see, change or delete a config file.
func TestRepoIgnoresConfigFiles(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()
	integrationID := newIntegration(t, pool, "resource-ignores-config")

	var configID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO integration_files (integration_id, path, role, kind, content)
		 VALUES ($1, 'integration.yaml', 'config', '', 'flows: []')
		 RETURNING id`, integrationID).Scan(&configID); err != nil {
		t.Fatalf("insert config file: %v", err)
	}

	if _, err := r.Create(ctx, integrationID, KindEnv, ".env.dev", "TOKEN=abc", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := r.ListByIntegration(ctx, integrationID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != ".env.dev" {
		t.Errorf("list = %+v, want only the resource", list)
	}

	if _, err := r.Get(ctx, integrationID, configID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get on a config file = %v, want ErrNotFound", err)
	}
	if err := r.Delete(ctx, integrationID, configID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete on a config file = %v, want ErrNotFound", err)
	}
}

func TestRepoDuplicatePathIsANameClash(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()
	integrationID := newIntegration(t, pool, "resource-duplicate-path")

	if _, err := r.Create(ctx, integrationID, KindEnv, ".env.dev", "a", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := r.Create(ctx, integrationID, KindEnv, ".env.dev", "b", ""); !errors.Is(err, ErrNameExists) {
		t.Errorf("second create = %v, want ErrNameExists", err)
	}
}

// Attribution is the point of the columns, so it is worth proving it survives a
// round trip — and that a write with no known actor stays unattributed instead
// of failing, which is the MCP and local-dev path.
func TestRepoRecordsAttribution(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()
	integrationID := newIntegration(t, pool, "resource-attribution")

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (subject, email, name) VALUES ($1, $2, $3) RETURNING id`,
		"subject-attribution", "someone@example.com", "Someone",
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) })

	created, err := r.Create(ctx, integrationID, KindEnv, ".env.dev", "A=1", userID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.CreatedBy == nil || *created.CreatedBy != userID {
		t.Fatalf("createdBy = %v, want %s", created.CreatedBy, userID)
	}
	if created.CreatedByEmail == nil || *created.CreatedByEmail != "someone@example.com" {
		t.Errorf("createdByEmail = %v, want the joined address", created.CreatedByEmail)
	}

	unattributed, err := r.Create(ctx, integrationID, KindEnv, ".env.other", "B=2", "")
	if err != nil {
		t.Fatalf("create without an actor: %v", err)
	}
	if unattributed.CreatedBy != nil {
		t.Errorf("createdBy = %v, want nil when no actor is known", unattributed.CreatedBy)
	}

	// Removing the user must not take their files with them.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	got, err := r.Get(ctx, integrationID, created.ID)
	if err != nil {
		t.Fatalf("get after the user was removed: %v", err)
	}
	if got.CreatedBy != nil {
		t.Errorf("createdBy = %v, want it nulled by ON DELETE SET NULL", got.CreatedBy)
	}
}
