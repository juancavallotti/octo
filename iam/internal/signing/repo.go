package signing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	cryptox "github.com/juancavallotti/octo/iam/internal/crypto"
)

const (
	// keyColumns is the canonical column list (and order) scanKey expects.
	keyColumns = "kid, algorithm, private_key, public_key, created_at, retire_after, expires_at"

	// rotateLockKey keys the advisory lock held while a new key is generated.
	// Advisory locks share one namespace per database, so what matters is only
	// that nothing else in this schema picks the same number — the user module's
	// bootstrap lock uses a different one.
	rotateLockKey = 5150082
)

// Repo persists the signing keyset to Postgres.
//
// The private half of every key is encrypted before it is written and decrypted on
// the way back, so a copy of the database — a dump, a snapshot, a replica somebody
// can read — does not by itself let anyone mint platform tokens. The cipher is
// required rather than optional: a keyset that silently stored its private keys in
// the clear because a setting was absent would be the one failure nothing here
// could report afterwards.
type Repo struct {
	pool   *pgxpool.Pool
	cipher *cryptox.Cipher
}

// NewRepo returns a Repo backed by the given pool, sealing private keys with
// cipher. A nil cipher is refused: see the type comment.
func NewRepo(pool *pgxpool.Pool, cipher *cryptox.Cipher) (*Repo, error) {
	if cipher == nil {
		return nil, fmt.Errorf("%w: a cipher is required to store signing keys", ErrInvalidConfig)
	}
	return &Repo{pool: pool, cipher: cipher}, nil
}

// Current returns the key that should sign now: the newest one that has not
// retired. ErrNoKey when there is none, which is the signal to rotate.
func (r *Repo) Current(ctx context.Context, now time.Time) (Key, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+keyColumns+` FROM iam_signing_keys
		  WHERE retire_after > $1
		  ORDER BY retire_after DESC
		  LIMIT 1`, now)
	k, err := r.scanKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Key{}, ErrNoKey
		}
		return Key{}, fmt.Errorf("signing repo: current: %w", err)
	}
	return k, nil
}

// Verifiers returns every key a token might still legitimately have been signed
// by: everything that has not expired, newest first. This is what the JWKS
// publishes, so it deliberately includes keys that have retired from signing —
// tokens they signed are still inside their lifetime.
func (r *Repo) Verifiers(ctx context.Context, now time.Time) ([]Key, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+keyColumns+` FROM iam_signing_keys
		  WHERE expires_at > $1
		  ORDER BY created_at DESC`, now)
	if err != nil {
		return nil, fmt.Errorf("signing repo: verifiers: %w", err)
	}
	defer rows.Close()

	keys := make([]Key, 0)
	for rows.Next() {
		k, err := r.scanKey(rows)
		if err != nil {
			return nil, fmt.Errorf("signing repo: verifiers: scan: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("signing repo: verifiers: %w", err)
	}
	return keys, nil
}

// Rotate installs a new signing key and returns it, deleting any key that has
// expired on the way.
//
// generate is called only if a key is actually needed — and it is called under an
// advisory lock, after the "is one needed" question has been asked a second time.
// The first ask happened outside the lock, in the caller, where it answers no for
// every request but the one that rotates; asking again here is what stops two
// replicas that both saw a retired key from installing two new ones.
//
// The lock is transaction-scoped, so it is released by the commit or by the
// deferred rollback and cannot be leaked by an early return.
func (r *Repo) Rotate(ctx context.Context, now time.Time, generate func() (Key, error)) (Key, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Key{}, fmt.Errorf("signing repo: rotate: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, rotateLockKey); err != nil {
		return Key{}, fmt.Errorf("signing repo: rotate: lock: %w", err)
	}

	// Someone may have rotated between the caller's check and this lock.
	existing, err := r.scanKey(tx.QueryRow(ctx,
		`SELECT `+keyColumns+` FROM iam_signing_keys
		  WHERE retire_after > $1
		  ORDER BY retire_after DESC
		  LIMIT 1`, now))
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return Key{}, fmt.Errorf("signing repo: rotate: recheck: %w", err)
	}

	fresh, err := generate()
	if err != nil {
		return Key{}, err
	}
	sealed, err := r.cipher.Encrypt(fresh.Private)
	if err != nil {
		return Key{}, fmt.Errorf("signing repo: rotate: seal private key: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO iam_signing_keys
		   (kid, algorithm, private_key, public_key, retire_after, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		fresh.KID, fresh.Algorithm, sealed, fresh.Public,
		fresh.RetireAfter, fresh.ExpiresAt)
	if err != nil {
		return Key{}, fmt.Errorf("signing repo: rotate: insert: %w", err)
	}

	// Swept here rather than on a ticker: rotation is the only moment the set
	// changes, so it is the only moment anything can have become droppable, and a
	// background goroutine for a once-a-month event would be a second mechanism
	// to reason about.
	if _, err := tx.Exec(ctx,
		`DELETE FROM iam_signing_keys WHERE expires_at <= $1`, now,
	); err != nil {
		return Key{}, fmt.Errorf("signing repo: rotate: sweep: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Key{}, fmt.Errorf("signing repo: rotate: commit: %w", err)
	}
	// Read back from what was generated rather than from the database: created_at
	// is the only field the row supplies, and nothing needs it.
	return fresh, nil
}

// scanKey reads one row in keyColumns order, opening the sealed private half.
//
// A key that will not decrypt is an error and not a skip. It means the stored
// keyset was written under a different KV_ENCRYPTION_KEY, and the honest thing is
// to say so: carrying on would quietly mint tokens under a new key while every
// token already issued stayed unverifiable, which is a worse outage than refusing.
func (r *Repo) scanKey(row pgx.Row) (Key, error) {
	var (
		k      Key
		sealed []byte
	)
	err := row.Scan(
		&k.KID, &k.Algorithm, &sealed, &k.Public,
		&k.CreatedAt, &k.RetireAfter, &k.ExpiresAt,
	)
	if err != nil {
		return Key{}, err
	}
	if k.Private, err = r.cipher.Decrypt(sealed); err != nil {
		return Key{}, fmt.Errorf(
			"signing repo: key %s will not decrypt, which means it was stored under a "+
				"different KV_ENCRYPTION_KEY: %w", k.KID, err)
	}
	return k, nil
}
