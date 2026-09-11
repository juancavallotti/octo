package user

import (
	"context"
	"errors"
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
// There are deliberately no CRUD tests. That Get returns the row Admit wrote,
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

// PINS: that pg_advisory_xact_lock actually serializes concurrent transactions.
//
// It is the only thing standing between several strangers signing in to a
// brand-new install at the same moment and an install with several admins — or,
// worse, with several accounts that were never supposed to exist. Nothing in Go
// can check that claim; a fake models the lock as "works".
//
// This is the allowlist's hardest case. Admit decides three things at once — is
// there an account, is there an administrator, and should this become one — and
// they are only true together if nothing can happen between them.
func TestAdmitIsRaceFree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	const contenders = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
		refused  int
		otherErr error
	)
	for i := range contenders {
		sub := "sub-" + string(rune('a'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := repo.Admit(ctx, sub, sub+"@example.com", sub)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrNotProvisioned):
				refused++
			case err != nil:
				if otherErr == nil {
					otherErr = err
				}
			case created:
				admitted++
			}
		}()
	}
	wg.Wait()

	if otherErr != nil {
		t.Fatalf("Admit: %v", otherErr)
	}
	// Exactly one of them is the first user. Everybody else meets the allowlist,
	// and — the part that would be silently wrong without the lock — gets no
	// account at all rather than an account with no roles.
	if admitted != 1 {
		t.Errorf("%d of %d concurrent strangers were admitted, want exactly 1", admitted, contenders)
	}
	if refused != contenders-1 {
		t.Errorf("%d were refused, want %d", refused, contenders-1)
	}

	users, _, err := repo.List(ctx, "", "", 100, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("the install has %d users, want exactly 1", len(users))
	}
	admins, err := repo.CountWithRole(ctx, RoleAdmin)
	if err != nil {
		t.Fatalf("CountWithRole: %v", err)
	}
	if admins != 1 {
		t.Errorf("the install has %d admins, want exactly 1", admins)
	}
}

// PINS: that the last-administrator rule holds under concurrency.
//
// Two administrators deleting themselves at the same moment could each observe
// two and then delete their own account, leaving none — and an install with no
// administrator hands the role to the next stranger who signs in, so this is a
// way to give the platform away rather than merely to lock it.
func TestTheLastAdministratorSurvivesConcurrentRemoval(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	first, _, err := repo.Admit(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Admit(first): %v", err)
	}
	second, err := repo.Create(ctx, "second@example.com", "Second")
	if err != nil {
		t.Fatalf("Create(second): %v", err)
	}
	if err := repo.Grant(ctx, second.ID, RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// Both try to go at once, by different routes — one deleting the account, the
	// other giving up the role. Either may win; both may not.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = repo.Delete(ctx, first.ID) }()
	go func() { defer wg.Done(); errs[1] = repo.Revoke(ctx, second.ID, RoleAdmin) }()
	wg.Wait()

	var refused int
	for _, err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, ErrLastAdmin):
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if refused != 1 {
		t.Errorf("%d of the two removals were refused, want exactly 1", refused)
	}

	admins, err := repo.CountWithRole(ctx, RoleAdmin)
	if err != nil {
		t.Fatalf("CountWithRole: %v", err)
	}
	if admins != 1 {
		t.Errorf("the install has %d admins, want exactly 1 left", admins)
	}
}

// PINS: that a row-value comparison orders by the whole tuple, and that an
// EXISTS filter does not reach inside the aggregate beside it.
//
// Both are what the paged listing bets on, and both are the kind of claim a fake
// would simply agree with.
//
// The first is keyset paging itself: `(created_at, id) > ($1, $2)` has to mean
// "after that row in this order", not "later timestamp OR larger id". Get it
// wrong and two people provisioned in the same microsecond are skipped or
// repeated, which is a bug nobody reproduces on a small directory.
//
// The second is why the role filter is an EXISTS rather than a condition on the
// joined rows. Restricting the join would also drop the other grants from
// array_agg, so a person filtered by one role would be reported as holding only
// that one — and the screen's role chips would show a set that is not theirs.
func TestPagingAndTheRoleFilterMeanWhatTheQueryAssumes(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Three rows sharing one created_at, so ordering can only come from the
	// tiebreak — which is the case the tuple comparison exists for.
	if _, err := repo.pool.Exec(ctx,
		`INSERT INTO users (email, created_at) VALUES
		   ('a@example.com', '2026-01-01T00:00:00Z'),
		   ('b@example.com', '2026-01-01T00:00:00Z'),
		   ('c@example.com', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	seen := map[string]bool{}
	cursor := ""
	for range 3 {
		page, next, err := repo.List(ctx, "", "", 1, cursor)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("page = %d rows, want 1", len(page))
		}
		if seen[page[0].ID] {
			t.Fatalf("%s came back on two pages", page[0].Email)
		}
		seen[page[0].ID] = true
		cursor = next
	}
	if len(seen) != 3 || cursor != "" {
		t.Errorf("saw %d of 3 rows, trailing cursor %q", len(seen), cursor)
	}

	// Two grants on one person, filtered by one of them.
	people, _, err := repo.List(ctx, "a@example.com", "", 1, "")
	if err != nil || len(people) != 1 {
		t.Fatalf("List: %v (%d rows)", err, len(people))
	}
	for _, role := range []Role{RoleAdmin, RoleMonitor} {
		if err := repo.Grant(ctx, people[0].ID, role, nil); err != nil {
			t.Fatalf("Grant(%s): %v", role, err)
		}
	}

	filtered, _, err := repo.List(ctx, "", RoleMonitor, 10, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered = %d rows, want the one person holding it", len(filtered))
	}
	if len(filtered[0].Roles) != 2 {
		t.Errorf("roles = %v, want both grants: the filter reached into the aggregate",
			filtered[0].Roles)
	}
}
