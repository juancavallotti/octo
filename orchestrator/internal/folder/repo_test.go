package folder

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// nonexistentID is a valid UUID that gen_random_uuid never produces, used for
// the not-found paths.
const nonexistentID = "00000000-0000-0000-0000-000000000000"

// newTestPool opens a pool against TEST_DATABASE_URL, skipping the test when it
// is unset so `go test ./...` stays green without a database.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run folder repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// createFolder inserts a folder and registers cleanup so tests do not leak data.
func createFolder(t *testing.T, r *Repo, name string, parentID *string) Folder {
	t.Helper()
	f, err := r.Create(context.Background(), name, parentID)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Delete(context.Background(), f.ID); err != nil && !errors.Is(err, ErrNotFound) {
			t.Errorf("cleanup delete folder %s: %v", f.ID, err)
		}
	})
	return f
}

// createIntegration inserts an integration and registers cleanup; membership
// rows cascade away with it.
func createIntegration(t *testing.T, pool *pgxpool.Pool, name string) integration.Integration {
	t.Helper()
	ir := integration.NewRepo(pool)
	it, err := ir.Create(context.Background(), name, "body")
	if err != nil {
		t.Fatalf("create integration: %v", err)
	}
	t.Cleanup(func() {
		if err := ir.Delete(context.Background(), it.ID); err != nil && !errors.Is(err, integration.ErrNotFound) {
			t.Errorf("cleanup delete integration %s: %v", it.ID, err)
		}
	})
	return it
}

func TestRepoCreateAndGet(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()

	root := createFolder(t, r, "root", nil)
	if root.ID == "" {
		t.Fatal("expected a generated id")
	}
	if root.ParentID != nil {
		t.Errorf("root parent_id = %v, want nil", *root.ParentID)
	}

	child := createFolder(t, r, "child", &root.ID)
	got, err := r.Get(ctx, child.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ParentID == nil || *got.ParentID != root.ID {
		t.Errorf("child parent_id = %v, want %s", got.ParentID, root.ID)
	}
}

func TestRepoList(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)

	created := createFolder(t, r, "list-me", nil)

	folders, err := r.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, f := range folders {
		if f.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created folder %s not present in list", created.ID)
	}
}

func TestRepoUpdateRenameAndMove(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()

	a := createFolder(t, r, "a", nil)
	b := createFolder(t, r, "b", nil)
	child := createFolder(t, r, "before", &a.ID)

	updated, err := r.Update(ctx, child.ID, "after", &b.ID)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "after" {
		t.Errorf("name = %q, want after", updated.Name)
	}
	if updated.ParentID == nil || *updated.ParentID != b.ID {
		t.Errorf("parent_id = %v, want %s", updated.ParentID, b.ID)
	}

	// Moving to a root clears parent_id.
	root, err := r.Update(ctx, child.ID, "after", nil)
	if err != nil {
		t.Fatalf("update to root: %v", err)
	}
	if root.ParentID != nil {
		t.Errorf("parent_id = %v, want nil", *root.ParentID)
	}
}

func TestRepoDeleteCascadesChildren(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()

	root, err := r.Create(ctx, "root", nil)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := r.Create(ctx, "child", &root.ID)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := r.Delete(ctx, root.ID); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	if _, err := r.Get(ctx, child.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("child get after cascade: got %v, want ErrNotFound", err)
	}
}

func TestRepoNotFound(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()

	if _, err := r.Get(ctx, nonexistentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: got %v, want ErrNotFound", err)
	}
	if _, err := r.Update(ctx, nonexistentID, "x", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("update: got %v, want ErrNotFound", err)
	}
	if err := r.Delete(ctx, nonexistentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete: got %v, want ErrNotFound", err)
	}
}

func TestRepoMembership(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()

	a := createFolder(t, r, "folder-a", nil)
	b := createFolder(t, r, "folder-b", nil)
	it := createIntegration(t, pool, "member")

	if err := r.AddIntegration(ctx, a.ID, it.ID); err != nil {
		t.Fatalf("add to a: %v", err)
	}
	inA, err := r.ListIntegrations(ctx, a.ID)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(inA) != 1 || inA[0].ID != it.ID {
		t.Fatalf("folder a members = %+v, want just %s", inA, it.ID)
	}

	// Single-folder: adding to b moves it out of a.
	if err := r.AddIntegration(ctx, b.ID, it.ID); err != nil {
		t.Fatalf("add to b: %v", err)
	}
	inA, _ = r.ListIntegrations(ctx, a.ID)
	if len(inA) != 0 {
		t.Errorf("folder a after move = %+v, want empty", inA)
	}
	inB, _ := r.ListIntegrations(ctx, b.ID)
	if len(inB) != 1 || inB[0].ID != it.ID {
		t.Errorf("folder b members = %+v, want just %s", inB, it.ID)
	}

	if err := r.RemoveIntegration(ctx, b.ID, it.ID); err != nil {
		t.Fatalf("remove from b: %v", err)
	}
	inB, _ = r.ListIntegrations(ctx, b.ID)
	if len(inB) != 0 {
		t.Errorf("folder b after remove = %+v, want empty", inB)
	}
}

func TestRepoMembershipNotFound(t *testing.T) {
	pool := newTestPool(t)
	r := NewRepo(pool)
	ctx := context.Background()

	folder := createFolder(t, r, "real-folder", nil)
	it := createIntegration(t, pool, "real-integration")

	// Unknown folder / integration -> ErrNotFound via FK violation.
	if err := r.AddIntegration(ctx, nonexistentID, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("add to missing folder: got %v, want ErrNotFound", err)
	}
	if err := r.AddIntegration(ctx, folder.ID, nonexistentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("add missing integration: got %v, want ErrNotFound", err)
	}
	// Removing a membership that does not exist -> ErrNotFound.
	if err := r.RemoveIntegration(ctx, folder.ID, it.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove absent membership: got %v, want ErrNotFound", err)
	}
}
