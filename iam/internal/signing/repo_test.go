package signing

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	cryptox "github.com/juancavallotti/octo/iam/internal/crypto"
)

// The only database test this package has, and it is here under the one exception
// in docs/coding-standards.md — see the comment on it.
//
// There are deliberately no CRUD tests. That a bytea column returns the bytes it
// was given, that a WHERE clause compares timestamps, that a DELETE deletes: all
// of that is Postgres's to guarantee and pgx's, not ours to re-assert on every
// pull request. The rotation policy those queries serve — which key signs, which
// keys stay published, when a new one is cut — is covered against the in-memory
// keyset in service_test.go, which needs no database.
//
// It skips without TEST_DATABASE_URL, so CI stands up no database for it.
//
// iam_signing_keys is emptied before and after, so point it at a throwaway
// database and never at one holding anything.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the iam signing repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	truncate(t, pool)
	t.Cleanup(func() { truncate(t, pool) })

	repo, err := NewRepo(pool, testCipher(t))
	if err != nil {
		t.Fatalf("NewRepo: %v", err)
	}
	return repo
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `TRUNCATE iam_signing_keys`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// testCipher is a fixed AES-256 key. Fixed rather than random so a stored keyset
// is readable across the cases in one run.
func testCipher(t *testing.T) *cryptox.Cipher {
	t.Helper()
	c, err := cryptox.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return c
}

// generator returns a Rotate callback producing a key through the real generation
// path, so what is stored is the real DER.
func generator(t *testing.T, now time.Time, lifetime time.Duration) func() (Key, error) {
	t.Helper()
	svc, err := NewService(&memRepo{}, Config{
		Issuer: "http://iam.test", Audience: "octo",
		TokenTTL: lifetime / 4, KeyLifetime: lifetime,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return func() (Key, error) { return svc.generate(now) }
}

// PINS: that pg_advisory_xact_lock actually serializes concurrent transactions.
//
// Every replica cuts a new key the moment it notices the current one has retired,
// and they all notice at once. Without the lock genuinely holding, a rotation
// installs one key per replica; each then signs with its own, and a token minted
// by one pod fails verification against the set another pod publishes — an
// intermittent auth failure with no bad input to reproduce it from. Nothing in Go
// can check that the lock works; a fake models it as working.
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
