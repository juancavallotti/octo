package user

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/iam/internal/role"
)

// nonexistentID is a well-formed UUID that no row carries, for the not-found
// paths. A malformed id is a different case and has its own assertions.
const nonexistentID = "00000000-0000-0000-0000-000000000000"

// newTestRepo opens a Repo against TEST_DATABASE_URL, skipping the test when it
// is not set. The database must have sql/schema.sql applied; see the comment on
// the `test` task in iam/Taskfile.yml for how to bring one up.
//
// Every case deletes the rows it made — and, because "the first user ever" is a
// property of the whole table, each case starts from an empty users table. So
// this must point at a throwaway database and never at one holding anything.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run iam user repo tests")
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

// truncate empties the two tables these tests own. user_roles goes with users by
// cascade, but it is named anyway so a failure reads as what it is.
func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `TRUNCATE user_roles, users CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// The whole admin bootstrap rests on this flag, so it is asserted against the
// real upsert rather than only through the fake: `xmax = 0` has to actually
// distinguish the INSERT branch from the DO UPDATE branch.
func TestUpsertReportsWhetherItCreatedTheRow(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	first, created, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !created {
		t.Error("created = false on the insert, want true")
	}

	again, created, err := repo.Upsert(ctx, "sub-1", "new@example.com", "New Name")
	if err != nil {
		t.Fatalf("Upsert(again): %v", err)
	}
	if created {
		t.Error("created = true on the refresh, want false")
	}
	if again.ID != first.ID {
		t.Errorf("id = %q on the refresh, want the stable %q", again.ID, first.ID)
	}
	if again.Email != "new@example.com" || again.Name != "New Name" {
		t.Errorf("got %q/%q, want the refreshed email and name", again.Email, again.Name)
	}
	if !again.LastLoginAt.After(first.LastLoginAt) && !again.LastLoginAt.Equal(first.LastLoginAt) {
		t.Errorf("last_login_at went backwards: %v then %v", first.LastLoginAt, again.LastLoginAt)
	}
}

// A LEFT JOIN over a user with no grants aggregates to {NULL} without the FILTER
// in rolesColumn, which would scan as one role with an empty name.
func TestGetReturnsNoRolesRatherThanOneEmptyOne(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	u, _, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Roles) != 0 {
		t.Errorf("roles = %#v, want none", got.Roles)
	}
}

func TestGrantRevokeAndRead(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	u, _, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	for _, r := range []role.Role{role.Monitor, role.Operator} {
		if err := repo.Grant(ctx, u.ID, r, nil); err != nil {
			t.Fatalf("Grant(%s): %v", r, err)
		}
	}
	// Twice, because the handler's PUT promises a state and not an event.
	if err := repo.Grant(ctx, u.ID, role.Monitor, nil); err != nil {
		t.Fatalf("Grant(monitor, again): %v", err)
	}

	got, err := repo.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Roles) != 2 || !got.HasRole(role.Monitor) || !got.HasRole(role.Operator) {
		t.Fatalf("roles = %v, want exactly monitor and operator", got.Roles)
	}

	if err := repo.Revoke(ctx, u.ID, role.Monitor); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	// Revoking what is not held is a no-op, for the same reason.
	if err := repo.Revoke(ctx, u.ID, role.Monitor); err != nil {
		t.Fatalf("Revoke(again): %v", err)
	}

	got, err = repo.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get(after revoke): %v", err)
	}
	if len(got.Roles) != 1 || !got.HasRole(role.Operator) {
		t.Errorf("roles = %v, want just operator", got.Roles)
	}
}

func TestGrantAttributesTheGrantor(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	admin, _, err := repo.Upsert(ctx, "sub-admin", "admin@example.com", "Admin")
	if err != nil {
		t.Fatalf("Upsert(admin): %v", err)
	}
	target, _, err := repo.Upsert(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("Upsert(target): %v", err)
	}

	if err := repo.Grant(ctx, target.ID, role.Developer, &admin.ID); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	var grantedBy string
	err = repo.pool.QueryRow(ctx,
		`SELECT granted_by FROM user_roles WHERE user_id = $1 AND role = $2`,
		target.ID, string(role.Developer),
	).Scan(&grantedBy)
	if err != nil {
		t.Fatalf("read granted_by: %v", err)
	}
	if grantedBy != admin.ID {
		t.Errorf("granted_by = %q, want %q", grantedBy, admin.ID)
	}
}

// An id from a URL path is whatever the caller typed. Both a well-formed id that
// matches nothing and a string Postgres cannot read as a uuid are requests for
// something that does not exist, and neither is a fault.
func TestUnknownAndMalformedIDsAreNotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	for _, id := range []string{nonexistentID, "not-a-uuid", ""} {
		t.Run("get "+id, func(t *testing.T) {
			if _, err := repo.Get(ctx, id); !errors.Is(err, ErrNotFound) {
				t.Errorf("Get(%q) error = %v, want ErrNotFound", id, err)
			}
		})
		t.Run("grant "+id, func(t *testing.T) {
			if err := repo.Grant(ctx, id, role.Monitor, nil); !errors.Is(err, ErrNotFound) {
				t.Errorf("Grant(%q) error = %v, want ErrNotFound", id, err)
			}
		})
	}

	if _, err := repo.GetBySubject(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBySubject error = %v, want ErrNotFound", err)
	}
}

func TestListIsOldestFirst(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	for _, sub := range []string{"sub-1", "sub-2", "sub-3"} {
		if _, _, err := repo.Upsert(ctx, sub, sub+"@example.com", sub); err != nil {
			t.Fatalf("Upsert(%s): %v", sub, err)
		}
	}

	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("List returned %d users, want 3", len(users))
	}
	for i := 1; i < len(users); i++ {
		if users[i].CreatedAt.Before(users[i-1].CreatedAt) {
			t.Errorf("user %d was created before user %d; want oldest first", i, i-1)
		}
	}
}

func TestEnsureFirstAdmin(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	first, created, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Upsert(first): %v", err)
	}
	granted, err := repo.EnsureFirstAdmin(ctx, first.ID, created)
	if err != nil {
		t.Fatalf("EnsureFirstAdmin(first): %v", err)
	}
	if !granted {
		t.Fatal("the first user was not made an admin")
	}

	// The second user gets nothing: an admin already exists.
	second, created, err := repo.Upsert(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("Upsert(second): %v", err)
	}
	if granted, err = repo.EnsureFirstAdmin(ctx, second.ID, created); err != nil {
		t.Fatalf("EnsureFirstAdmin(second): %v", err)
	}
	if granted {
		t.Error("the second user was made an admin")
	}

	// And a later sign-in by the first user grants nothing, because it created no
	// row — which is what stops a revoked admin from restoring themselves.
	_, created, err = repo.Upsert(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Upsert(first, again): %v", err)
	}
	if granted, err = repo.EnsureFirstAdmin(ctx, first.ID, created); err != nil {
		t.Fatalf("EnsureFirstAdmin(first, again): %v", err)
	}
	if granted {
		t.Error("a later sign-in re-granted admin")
	}
}

// Two people signing in to a brand-new install at the same moment must produce
// exactly one admin. The advisory lock is the only thing making that true, so it
// is asserted against a real database and with -race.
func TestEnsureFirstAdminIsRaceFree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	const contenders = 8
	ids := make([]string, contenders)
	createdFlags := make([]bool, contenders)
	for i := range contenders {
		sub := "sub-" + string(rune('a'+i))
		u, created, err := repo.Upsert(ctx, sub, sub+"@example.com", sub)
		if err != nil {
			t.Fatalf("Upsert(%s): %v", sub, err)
		}
		ids[i], createdFlags[i] = u.ID, created
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
			ok, err := repo.EnsureFirstAdmin(ctx, ids[i], createdFlags[i])
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

	admins, err := repo.CountWithRole(ctx, role.Admin)
	if err != nil {
		t.Fatalf("CountWithRole: %v", err)
	}
	if admins != 1 {
		t.Errorf("the install has %d admins, want exactly 1", admins)
	}
}

func TestCountWithRole(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if n, err := repo.CountWithRole(ctx, role.Admin); err != nil || n != 0 {
		t.Fatalf("CountWithRole on an empty install = %d, %v; want 0, nil", n, err)
	}

	for _, sub := range []string{"sub-1", "sub-2"} {
		u, _, err := repo.Upsert(ctx, sub, sub+"@example.com", sub)
		if err != nil {
			t.Fatalf("Upsert(%s): %v", sub, err)
		}
		if err := repo.Grant(ctx, u.ID, role.Admin, nil); err != nil {
			t.Fatalf("Grant(%s): %v", sub, err)
		}
	}

	if n, err := repo.CountWithRole(ctx, role.Admin); err != nil || n != 2 {
		t.Errorf("CountWithRole = %d, %v; want 2, nil", n, err)
	}
	if n, err := repo.CountWithRole(ctx, role.Monitor); err != nil || n != 0 {
		t.Errorf("CountWithRole(monitor) = %d, %v; want 0, nil", n, err)
	}
}

// Deleting a user must take their grants with them, or the table accumulates rows
// pointing at nobody.
func TestDeletingAUserCascadesTheirGrants(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	u, _, err := repo.Upsert(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.Grant(ctx, u.ID, role.Developer, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	if _, err := repo.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var orphans int
	if err := repo.pool.QueryRow(ctx,
		`SELECT count(*) FROM user_roles WHERE user_id = $1`, u.ID,
	).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d grants survived the user, want 0", orphans)
	}
}
