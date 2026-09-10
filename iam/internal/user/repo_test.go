package user

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The only two database tests this package has, and both are here under the one
// exception in docs/coding-standards.md: they pin a *specific non-obvious
// behavior of Postgres* that correctness bets on, where what would break is our
// reading of it rather than our code.
//
// There are deliberately no CRUD tests. That Get returns the row Upsert wrote,
// that a cascade cascades, that a WHERE clause filters — those assert that
// Postgres and pgx work, which is not ours to establish. The rules that ARE ours
// (who may be granted what, what the last admin may not do) are covered against
// the in-memory repository in service_test.go, which needs no database and runs
// everywhere.
//
// Both skip without TEST_DATABASE_URL, so CI does not stand up a database for
// them; run them deliberately when touching repo.go. The database must have
// sql/schema.sql applied — see the `test` task in iam/Taskfile.yml.
//
// Rows are DELETED from users and user_roles, so point it at a throwaway database
// and never at one holding anything.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the iam user repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	truncate(t, pool)
	t.Cleanup(func() { truncate(t, pool) })
	return NewRepo(pool)
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `TRUNCATE user_roles, users CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// PINS: that `xmax = 0` in an upsert's RETURNING actually distinguishes the
// INSERT branch from the DO UPDATE branch.
//
// Being wrong about this is silent, which is why it earns a test. The whole admin
// bootstrap reads that one boolean: if it were always true, every sign-in would
// re-grant admin and a deliberate demotion could be undone by logging out and in
// again; if always false, a fresh install would never get an admin at all and
// nobody could grant one. Both compile, both pass every fake, and neither
// announces itself.
func TestUpsertReportsWhetherItCreatedTheRow(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, created, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First"); err != nil {
		t.Fatalf("Upsert: %v", err)
	} else if !created {
		t.Error("created = false on the insert, want true")
	}

	if _, created, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First"); err != nil {
		t.Fatalf("Upsert(again): %v", err)
	} else if created {
		t.Error("created = true on the refresh, want false")
	}
}

// PINS: that pg_advisory_xact_lock actually serializes concurrent transactions.
//
// It is the only thing standing between two people signing in to a brand-new
// install at the same moment and an install with two admins — or, if the lock
// silently did nothing, with however many raced. Nothing in Go can check that
// claim; a fake models the lock as "works".
func TestEnsureFirstAdminIsRaceFree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	const contenders = 8
	ids := make([]string, contenders)
	created := make([]bool, contenders)
	for i := range contenders {
		sub := "sub-" + string(rune('a'+i))
		u, c, err := repo.Upsert(ctx, sub, sub+"@example.com", sub)
		if err != nil {
			t.Fatalf("Upsert(%s): %v", sub, err)
		}
		ids[i], created[i] = u.ID, c
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		grants   int
		firstErr error
	)
	for i := range contenders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := repo.EnsureFirstAdmin(ctx, ids[i], created[i])
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if ok {
				grants++
			}
		}(i)
	}
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("EnsureFirstAdmin: %v", firstErr)
	}
	if grants != 1 {
		t.Errorf("EnsureFirstAdmin granted %d times concurrently, want exactly 1", grants)
	}
	admins, err := repo.CountWithRole(ctx, RoleAdmin)
	if err != nil {
		t.Fatalf("CountWithRole: %v", err)
	}
	if admins != 1 {
		t.Errorf("the install has %d admins, want exactly 1", admins)
	}
}
