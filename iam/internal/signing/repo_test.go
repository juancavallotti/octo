package signing

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestRepo opens a Repo against TEST_DATABASE_URL, skipping the test when it
// is not set. The database must have sql/schema.sql applied; see the comment on
// the `test` task in iam/Taskfile.yml.
//
// iam_signing_keys is emptied before and after each case, so this must point at a
// throwaway database and never at one holding anything.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run iam signing repo tests")
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
	if _, err := pool.Exec(context.Background(), `TRUNCATE iam_signing_keys`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// generator returns a Rotate callback producing a key with the given horizons,
// through the real generation path so the DER encodings are the real ones.
func generator(t *testing.T, now time.Time, lifetime time.Duration) func() (Key, error) {
	t.Helper()
	// The token lifetime is derived from the key's rather than fixed, so a case
	// can ask for a short-lived key without tripping the coherence check.
	svc, err := NewService(&memRepo{}, Config{
		Issuer: "http://iam.test", Audience: "octo",
		TokenTTL: lifetime / 4, KeyLifetime: lifetime,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return func() (Key, error) { return svc.generate(now) }
}

func TestCurrentOnAnEmptySetIsErrNoKey(t *testing.T) {
	repo := newTestRepo(t)

	if _, err := repo.Current(context.Background(), time.Now()); !errors.Is(err, ErrNoKey) {
		t.Errorf("Current() error = %v, want ErrNoKey", err)
	}
}

// A round trip through bytea has to preserve the DER exactly, or the key comes
// back unparseable and every token fails at signing time.
func TestRotateStoresAKeyThatComesBackIntact(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	fresh, err := repo.Rotate(ctx, now, generator(t, now, 24*time.Hour))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	got, err := repo.Current(ctx, now)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got.KID != fresh.KID {
		t.Errorf("kid = %q, want %q", got.KID, fresh.KID)
	}
	if got.Algorithm != signingAlgorithm {
		t.Errorf("algorithm = %q, want %q", got.Algorithm, signingAlgorithm)
	}
	if string(got.Private) != string(fresh.Private) {
		t.Error("the private key did not survive the round trip")
	}
	if string(got.Public) != string(fresh.Public) {
		t.Error("the public key did not survive the round trip")
	}
	// The horizons are stored to microsecond precision by Postgres, so they are
	// compared at that resolution rather than for exact equality.
	if !got.RetireAfter.Round(time.Microsecond).Equal(fresh.RetireAfter.Round(time.Microsecond)) {
		t.Errorf("retire_after = %v, want %v", got.RetireAfter, fresh.RetireAfter)
	}
	if !got.ExpiresAt.Round(time.Microsecond).Equal(fresh.ExpiresAt.Round(time.Microsecond)) {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, fresh.ExpiresAt)
	}
}

// Current must not hand back a retired key, and Verifiers must still publish it.
func TestCurrentAndVerifiersUseTheirOwnHorizon(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	fresh, err := repo.Rotate(ctx, now, generator(t, now, time.Hour))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// Past retire_after but well inside expires_at.
	afterRetirement := fresh.RetireAfter.Add(time.Minute)
	if _, err := repo.Current(ctx, afterRetirement); !errors.Is(err, ErrNoKey) {
		t.Errorf("Current(after retirement) error = %v, want ErrNoKey", err)
	}
	verifiers, err := repo.Verifiers(ctx, afterRetirement)
	if err != nil {
		t.Fatalf("Verifiers: %v", err)
	}
	if len(verifiers) != 1 || verifiers[0].KID != fresh.KID {
		t.Errorf("Verifiers(after retirement) = %d keys, want the retired one", len(verifiers))
	}

	// Past expires_at it goes.
	verifiers, err = repo.Verifiers(ctx, fresh.ExpiresAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verifiers(after expiry): %v", err)
	}
	if len(verifiers) != 0 {
		t.Errorf("Verifiers(after expiry) = %d keys, want none", len(verifiers))
	}
}

func TestRotateSweepsExpiredKeys(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	start := time.Now()
	old, err := repo.Rotate(ctx, start, generator(t, start, time.Hour))
	if err != nil {
		t.Fatalf("Rotate(old): %v", err)
	}

	// Long after the first key expired, so the next rotation should drop it.
	later := old.ExpiresAt.Add(time.Hour)
	if _, err := repo.Rotate(ctx, later, generator(t, later, time.Hour)); err != nil {
		t.Fatalf("Rotate(new): %v", err)
	}

	var remaining int
	if err := repo.pool.QueryRow(ctx,
		`SELECT count(*) FROM iam_signing_keys WHERE kid = $1`, old.KID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Errorf("the expired key is still stored")
	}
}

// Rotate must not install a second key when one already signs, or two replicas
// starting together would each mint their own and each publish a set the other
// disagrees with.
func TestRotateReusesAKeyThatAlreadySigns(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	first, err := repo.Rotate(ctx, now, generator(t, now, 24*time.Hour))
	if err != nil {
		t.Fatalf("Rotate(first): %v", err)
	}

	generated := false
	second, err := repo.Rotate(ctx, now, func() (Key, error) {
		generated = true
		return generator(t, now, 24*time.Hour)()
	})
	if err != nil {
		t.Fatalf("Rotate(second): %v", err)
	}
	if generated {
		t.Error("Rotate generated a key while a valid one already existed")
	}
	if second.KID != first.KID {
		t.Errorf("Rotate returned %q, want the existing %q", second.KID, first.KID)
	}
}

// The advisory lock is the only thing that makes concurrent rotation safe, so it
// is asserted against a real database and with -race.
func TestRotateIsRaceFree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	now := time.Now()

	const contenders = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		kids     = map[string]struct{}{}
		firstErr error
	)
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, err := repo.Rotate(ctx, now, generator(t, now, 24*time.Hour))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			kids[k.KID] = struct{}{}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("Rotate: %v", firstErr)
	}
	if len(kids) != 1 {
		t.Errorf("concurrent rotation produced %d distinct keys, want 1", len(kids))
	}

	var stored int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM iam_signing_keys`).Scan(&stored); err != nil {
		t.Fatalf("count: %v", err)
	}
	if stored != 1 {
		t.Errorf("%d keys were stored, want 1", stored)
	}
}

// The whole design rests on the key being shared, so two independently
// constructed services over the same database must sign with the same key.
func TestTwoServicesOverOneDatabaseShareTheKey(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cfg := Config{Issuer: "http://iam.test", Audience: "octo"}
	first, err := NewService(repo, cfg)
	if err != nil {
		t.Fatalf("NewService(first): %v", err)
	}
	second, err := NewService(NewRepo(repo.pool), cfg)
	if err != nil {
		t.Fatalf("NewService(second): %v", err)
	}

	a, err := first.Mint(ctx, "user-1", struct{}{})
	if err != nil {
		t.Fatalf("Mint(first): %v", err)
	}
	b, err := second.Mint(ctx, "user-1", struct{}{})
	if err != nil {
		t.Fatalf("Mint(second): %v", err)
	}

	if kidOf(t, a.Value) != kidOf(t, b.Value) {
		t.Error("two replicas signed with different keys")
	}

	// And each one's token verifies against the other's published set, which is
	// the property a caller actually depends on.
	verifyAgainstJWKS(t, second, a.Value)
	verifyAgainstJWKS(t, first, b.Value)
}
