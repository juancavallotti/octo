package kv

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestRepo opens a pool against TEST_DATABASE_URL, skipping the test when it is
// unset so `go test ./...` stays green without a database. Each test uses a fresh
// random deployment id so rows never collide and cleanup is a single delete.
func newTestRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run kv repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	repo := NewRepo(pool)
	deploymentID := uuid.NewString()
	t.Cleanup(func() {
		_ = repo.DeleteByDeployment(context.Background(), deploymentID)
		pool.Close()
	})
	return repo, deploymentID
}

func TestRepoCreateGetUpdate(t *testing.T) {
	repo, dep := newTestRepo(t)
	ctx := context.Background()

	v1, err := repo.Write(ctx, dep, "user", "k", []byte("a"), 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("version = %d, want 1", v1)
	}

	value, version, ok, err := repo.Get(ctx, dep, "user", "k")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if string(value) != "a" || version != 1 {
		t.Fatalf("got value=%q version=%d, want a/1", value, version)
	}

	v2, err := repo.Write(ctx, dep, "user", "k", []byte("b"), v1)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("version = %d, want 2", v2)
	}
}

func TestRepoStaleWriteConflicts(t *testing.T) {
	repo, dep := newTestRepo(t)
	ctx := context.Background()
	v1, _ := repo.Write(ctx, dep, "user", "k", []byte("a"), 0)
	if _, err := repo.Write(ctx, dep, "user", "k", []byte("b"), v1); err != nil {
		t.Fatalf("write: %v", err)
	}
	// v1 is now stale.
	if _, err := repo.Write(ctx, dep, "user", "k", []byte("c"), v1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale write: err = %v, want ErrVersionConflict", err)
	}
}

func TestRepoCreateOverExistingConflicts(t *testing.T) {
	repo, dep := newTestRepo(t)
	ctx := context.Background()
	if _, err := repo.Write(ctx, dep, "user", "k", []byte("a"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Write(ctx, dep, "user", "k", []byte("b"), 0); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("create over existing: err = %v, want ErrVersionConflict", err)
	}
}

func TestRepoNamespacesIsolated(t *testing.T) {
	repo, dep := newTestRepo(t)
	ctx := context.Background()
	if _, err := repo.Write(ctx, dep, "system", "k", []byte("sys"), 0); err != nil {
		t.Fatalf("write system: %v", err)
	}
	// Same key under "user" is independent (create succeeds with version 0).
	if _, err := repo.Write(ctx, dep, "user", "k", []byte("usr"), 0); err != nil {
		t.Fatalf("write user: %v", err)
	}
	value, _, _, _ := repo.Get(ctx, dep, "system", "k")
	if string(value) != "sys" {
		t.Fatalf("system value = %q, want sys", value)
	}
}

func TestRepoDeleteAndByDeployment(t *testing.T) {
	repo, dep := newTestRepo(t)
	ctx := context.Background()
	v1, _ := repo.Write(ctx, dep, "user", "k", []byte("a"), 0)

	// Wrong version conflicts; correct version deletes.
	if err := repo.Delete(ctx, dep, "user", "k", v1+1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("delete wrong version: err = %v, want ErrVersionConflict", err)
	}
	if err := repo.Delete(ctx, dep, "user", "k", v1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, ok, _ := repo.Get(ctx, dep, "user", "k"); ok {
		t.Fatal("key present after delete")
	}

	// DeleteByDeployment removes everything for the deployment.
	_, _ = repo.Write(ctx, dep, "user", "a", []byte("1"), 0)
	_, _ = repo.Write(ctx, dep, "system", "b", []byte("2"), 0)
	if err := repo.DeleteByDeployment(ctx, dep); err != nil {
		t.Fatalf("delete by deployment: %v", err)
	}
	if _, _, ok, _ := repo.Get(ctx, dep, "user", "a"); ok {
		t.Fatal("user/a present after DeleteByDeployment")
	}
}
