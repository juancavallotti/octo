package retention

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// sweepLockID keys the advisory lock the sweep runs under. The value is
// arbitrary — advisory locks share one namespace per database and Postgres
// attaches no meaning to the number — but it must never change, or a rolling
// upgrade would have the two versions taking different locks and running
// concurrently, which is the one thing the lock exists to prevent.
const sweepLockID int64 = 4919772001

// sweepLock serializes retention sweeps across everything sharing the database:
// the nightly job, an operator pressing Run now, and any extra replicas of this
// service. Two sweeps at once are not incorrect — a delete that finds its rows
// already gone is a no-op — but they contend for the same locks against an
// ingest path they are supposed to stay out of the way of.
type sweepLock struct {
	pool *pgxpool.Pool
}

// tryLock takes the sweep lock. It returns a release function and true when the
// lock is held, and false when another sweep already holds it. Only the caller
// that acquired it may release it.
//
// This is a session lock rather than a transaction one, and so it needs a
// connection of its own: pg_advisory_unlock only releases a lock taken by the
// same session, and a pooled call issued on some other connection would return
// false and leave the lock held until the holding connection happened to close.
// A transaction-scoped lock would avoid all that and is the wrong tool — it
// would tie the whole sweep to one transaction, which is exactly what the
// batched deletes exist to avoid.
func (l *sweepLock) tryLock(ctx context.Context) (func(), bool, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("retention: acquire a connection for the sweep lock: %w", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, sweepLockID).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("retention: take the sweep lock: %w", err)
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}

	return func() {
		// WithoutCancel because the common reason a sweep ends early is that its
		// context was cancelled, and that is precisely when the lock most needs
		// releasing. Releasing it is also not strictly required — the lock dies
		// with the session, and a connection this one discards would end it — but
		// pooled connections are long-lived, so leaving it would block the next
		// sweep for as long as the pool kept the connection around.
		if _, err := conn.Exec(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock($1)`, sweepLockID); err != nil {
			slog.Error("retention: release the sweep lock", "err", err)
		}
		conn.Release()
	}, true, nil
}
