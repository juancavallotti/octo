package integration

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// nonexistentID is a valid UUID that gen_random_uuid never produces, used for
// the not-found paths.
const nonexistentID = "00000000-0000-0000-0000-000000000000"

// newTestRepo opens a Repo against TEST_DATABASE_URL, skipping the test when it
// is unset so `go test ./...` stays green without a database.
func newTestRepo(t *testing.T) *Repo {
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
	return NewRepo(pool)
}

// createIntegration inserts a row and registers cleanup so tests do not leak
// data between runs.
func createIntegration(t *testing.T, r *Repo, name, definition string) Integration {
	t.Helper()
	it, err := r.Create(context.Background(), name, definition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Delete(context.Background(), it.ID); err != nil && !errors.Is(err, ErrNotFound) {
			t.Errorf("cleanup delete %s: %v", it.ID, err)
		}
	})
	return it
}

func TestRepoCreateAndGet(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "create-and-get", "definition-body")
	if created.ID == "" {
		t.Fatal("expected a generated id")
	}
	if created.LastUpdated.IsZero() {
		t.Error("expected last_updated to be stamped")
	}

	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "create-and-get" || got.Definition != "definition-body" {
		t.Errorf("got %+v, want name/definition to round-trip", got)
	}
}

func TestRepoList(t *testing.T) {
	r := newTestRepo(t)

	created := createIntegration(t, r, "list-me", "body")

	items, err := r.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, it := range items {
		if it.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created integration %s not present in list", created.ID)
	}
}

func TestRepoUpdate(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "before", "old-body")

	updated, err := r.Update(ctx, created.ID, "after", "new-body")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "after" || updated.Definition != "new-body" {
		t.Errorf("got %+v, want updated name/definition", updated)
	}
	if !updated.LastUpdated.After(created.LastUpdated) {
		t.Errorf("expected last_updated to advance: created=%v updated=%v",
			created.LastUpdated, updated.LastUpdated)
	}
}

func TestRepoDelete(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "delete-me", "body")

	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound after delete", err)
	}
}

func TestRepoNotFound(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	if _, err := r.Get(ctx, nonexistentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: got %v, want ErrNotFound", err)
	}
	if _, err := r.Update(ctx, nonexistentID, "x", "y"); !errors.Is(err, ErrNotFound) {
		t.Errorf("update: got %v, want ErrNotFound", err)
	}
	if err := r.Delete(ctx, nonexistentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete: got %v, want ErrNotFound", err)
	}
}
