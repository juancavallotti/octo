package retention

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testKey is a key of this suite's own, so the store tests cannot read or
// overwrite the installation's actual policy.
const testKey = "retention_test_88888888"

// newTestPool opens a pool against TEST_DATABASE_URL, skipping when it is unset
// so `go test ./...` stays green without a database.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run retention store tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM site_settings WHERE key = $1`, testKey)
		pool.Close()
	})
	return pool
}

func TestStoreGetAbsentKey(t *testing.T) {
	store := NewStore(newTestPool(t))

	value, ok, err := store.Get(context.Background(), testKey)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Fatal("absent key reported as present")
	}
	if value != nil {
		t.Fatalf("value = %s, want nil", value)
	}
}

func TestStorePutOverwrites(t *testing.T) {
	store := NewStore(newTestPool(t))
	ctx := context.Background()

	if err := store.Put(ctx, testKey, json.RawMessage(`{"logsDays":30}`)); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := store.Put(ctx, testKey, json.RawMessage(`{"logsDays":90}`)); err != nil {
		t.Fatalf("second put: %v", err)
	}

	value, ok, err := store.Get(ctx, testKey)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	var got stored
	if err := json.Unmarshal(value, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.LogsDays != 90 {
		t.Fatalf("logsDays = %d, want 90 (the second put should win)", got.LogsDays)
	}
}

// The repo owns the policy's shape, so the round trip worth pinning is
// stored → jsonb → stored rather than the raw bytes: jsonb is parsed on the way
// in, and what comes back is Postgres's rendering of the value.
//
// This one exercises the real settings key, because that is the key the repo
// hard-codes. It restores whatever was there on the way out.
func TestRepoAbsentRowThenRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	repo := NewRepo(pool)
	ctx := context.Background()

	before, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("read the existing policy: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Put(context.Background(), before)
	})

	if _, err := pool.Exec(ctx,
		`DELETE FROM site_settings WHERE key = $1`, settingsKey); err != nil {
		t.Fatalf("clear the policy row: %v", err)
	}

	// An installation that never configured retention has no row at all, and
	// that has to read as "keep everything" rather than as a failure.
	absent, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get with no row: %v", err)
	}
	if absent.toPolicy().Enabled() {
		t.Fatalf("an absent row decoded to an enabled policy: %+v", absent)
	}

	when := time.Date(2032, 5, 6, 11, 0, 0, 0, time.UTC)
	if err := repo.Put(ctx, stored{LogsDays: 30, TracesDays: 7, UpdatedAt: when}); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LogsDays != 30 || got.TracesDays != 7 {
		t.Fatalf("days = (%d, %d), want (30, 7)", got.LogsDays, got.TracesDays)
	}
	if !got.UpdatedAt.Equal(when) {
		t.Fatalf("updatedAt = %v, want %v", got.UpdatedAt, when)
	}
}
