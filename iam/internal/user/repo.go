package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// userColumns is the canonical column list (and order) scanUser expects, kept
	// in one place so reads and RETURNING clauses stay in sync. It is qualified
	// with the table alias every read below uses, because every read joins.
	userColumns = "u.id, u.subject, u.email, u.name, u.created_at, u.last_login_at"

	// rolesColumn aggregates the joined grants into one text array. FILTER drops
	// the single NULL row a LEFT JOIN produces for a user with no grants, which
	// would otherwise aggregate to {NULL} and scan as a one-element slice holding
	// an empty role.
	rolesColumn = `COALESCE(array_agg(ur.role) FILTER (WHERE ur.role IS NOT NULL), '{}')`

	// selectUsers is the shared read. Grouping by u.id alone is enough: it is the
	// primary key, so Postgres knows the other selected columns are functionally
	// dependent on it.
	selectUsers = `SELECT ` + userColumns + `, ` + rolesColumn + `
		 FROM users u LEFT JOIN user_roles ur ON ur.user_id = u.id`

	// bootstrapLockKey keys the advisory lock taken while deciding whether a user
	// is the first ever to sign in. An arbitrary constant — advisory locks share
	// one namespace per database, so what matters is only that nothing else in
	// this schema picks the same number.
	bootstrapLockKey = 5150081

	// pgInvalidTextRepresentation is what Postgres reports when a value cannot be
	// parsed as the column's type — a user id that is not a UUID, here. It is a
	// caller's mistake rather than a fault, so it is translated to ErrNotFound
	// instead of surfacing as a 500.
	pgInvalidTextRepresentation = "22P02"
)

// Repo persists users and their role grants to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Upsert provisions the user identified by subject, or refreshes the email/name
// and last_login_at of an existing one, returning the resulting row. The
// generated id is stable across logins.
//
// This is the same statement the orchestrator's user repo issues, deliberately:
// while both services are wired up, either path must be able to run first and the
// other must find the row it made rather than making a second one.
//
// The returned User carries no roles — this is the write, and the caller reads
// the user back once the grants it may add are settled.
//
// The second return reports whether this call created the row, as opposed to
// refreshing one that already existed. It is what makes "the first sign-in ever"
// a fact about this statement rather than a guess from the table's contents, and
// the admin bootstrap below depends on it. `xmax = 0` is the standard way to ask
// an upsert which branch it took: on the INSERT branch the new tuple has no
// deleting transaction, while the DO UPDATE branch stamps the updating one.
func (r *Repo) Upsert(ctx context.Context, subject, email, name string) (User, bool, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (subject, email, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (subject) DO UPDATE SET
		   email = EXCLUDED.email,
		   name = EXCLUDED.name,
		   last_login_at = now()
		 RETURNING id, subject, email, name, created_at, last_login_at, (xmax = 0)`,
		subject, email, name,
	)
	var (
		u       User
		created bool
	)
	err := row.Scan(&u.ID, &u.Subject, &u.Email, &u.Name, &u.CreatedAt, &u.LastLoginAt, &created)
	if err != nil {
		return User{}, false, fmt.Errorf("user repo: upsert: %w", err)
	}
	return u, created, nil
}

// Get returns the user by id with their granted roles, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, id string) (User, error) {
	row := r.pool.QueryRow(ctx, selectUsers+` WHERE u.id = $1 GROUP BY u.id`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidTextRepresentation(err) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("user repo: get: %w", err)
	}
	return u, nil
}

// GetBySubject returns the user by OIDC subject with their granted roles, or
// ErrNotFound.
func (r *Repo) GetBySubject(ctx context.Context, subject string) (User, error) {
	row := r.pool.QueryRow(ctx, selectUsers+` WHERE u.subject = $1 GROUP BY u.id`, subject)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("user repo: get by subject: %w", err)
	}
	return u, nil
}

// List returns every user with their granted roles, oldest first — which is the
// order they signed in, and puts whoever set the platform up at the top.
func (r *Repo) List(ctx context.Context) ([]User, error) {
	rows, err := r.pool.Query(ctx, selectUsers+` GROUP BY u.id ORDER BY u.created_at, u.id`)
	if err != nil {
		return nil, fmt.Errorf("user repo: list: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("user repo: list: scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user repo: list: %w", err)
	}
	return users, nil
}

// Grant gives userID the role r, attributed to grantedBy (nil when the service
// itself is the grantor). Granting a role the user already holds is a no-op
// rather than an error: the caller asked for a state, and the state holds.
//
// A userID that names no user is ErrNotFound — surfaced from the foreign key
// rather than pre-checked, so a user deleted between the check and the write
// cannot produce a grant pointing at nothing.
func (r *Repo) Grant(ctx context.Context, userID string, granted Role, grantedBy *string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role, granted_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, role) DO NOTHING`,
		userID, string(granted), grantedBy,
	)
	if err != nil {
		if isForeignKeyViolation(err) || isInvalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user repo: grant: %w", err)
	}
	return nil
}

// Revoke removes the role r from userID. Revoking a role the user does not hold
// is a no-op, for the same reason granting one twice is.
func (r *Repo) Revoke(ctx context.Context, userID string, revoked Role) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND role = $2`,
		userID, string(revoked),
	)
	if err != nil {
		if isInvalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user repo: revoke: %w", err)
	}
	return nil
}

// CountWithRole returns how many users hold the given role. The admin count is
// what stands between an install and having nobody who can grant anything, so it
// is the check the revoke path makes before removing the last one.
func (r *Repo) CountWithRole(ctx context.Context, held Role) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM user_roles WHERE role = $1`, string(held),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("user repo: count with role: %w", err)
	}
	return n, nil
}

// EnsureFirstAdmin grants platform:admin to a user who was just created, when
// the install has no admin at all, and reports whether it did. Without it a fresh
// install has nobody who can grant a role to anyone, including the role that
// would fix that.
//
// Two conditions, and each rules out a different failure:
//
//   - The row was created by this very sign-in (the caller passes what Upsert
//     reported). This is what stops the grant from re-firing: an admin who
//     revoked their own role on a single-user install would otherwise get it back
//     by logging out and in again, undoing a deliberate act.
//   - Nobody currently holds platform:admin. Not "nobody holds any role" and not
//     "this is the only user" — under the lock below, two people signing in to a
//     brand-new install at the same moment then produce exactly one admin, where
//     counting users would have produced two, or none.
//
// The consequence worth stating: an install that loses every admin hands the role
// to the next person who signs in for the first time. That is deliberate. They
// have already satisfied the identity provider, and the alternative is an install
// no one can administer and that no amount of signing in can repair.
//
// The cheap check runs first and unlocked, because it is false for every sign-in
// after the first and there is no reason to serialise those behind a lock.
func (r *Repo) EnsureFirstAdmin(ctx context.Context, userID string, created bool) (bool, error) {
	if !created {
		return false, nil
	}
	eligible, err := r.firstAdminEligible(ctx, r.pool, userID)
	if err != nil || !eligible {
		return false, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("user repo: ensure first admin: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	// Transaction-scoped, so it is released by the commit or the rollback above
	// and cannot be leaked by an early return.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey); err != nil {
		return false, fmt.Errorf("user repo: ensure first admin: lock: %w", err)
	}

	// Retaken under the lock: the unlocked read above may have been answered
	// before another replica's grant committed.
	eligible, err = r.firstAdminEligible(ctx, tx, userID)
	if err != nil || !eligible {
		return false, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO user_roles (user_id, role) VALUES ($1, $2)
		 ON CONFLICT (user_id, role) DO NOTHING`,
		userID, string(RoleAdmin),
	); err != nil {
		return false, fmt.Errorf("user repo: ensure first admin: grant: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("user repo: ensure first admin: commit: %w", err)
	}
	return true, nil
}

// querier is the subset of pgx both a pool and a transaction satisfy, so the
// eligibility check can be asked the same question inside and outside the lock.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// firstAdminEligible reports whether the install has no admin and userID names a
// real user. Both halves are one round trip, because asking them separately would
// let either answer change between the two.
func (r *Repo) firstAdminEligible(ctx context.Context, q querier, userID string) (bool, error) {
	var eligible bool
	err := q.QueryRow(ctx,
		`SELECT NOT EXISTS (SELECT 1 FROM user_roles WHERE role = $2)
		    AND EXISTS (SELECT 1 FROM users WHERE id = $1)`,
		userID, string(RoleAdmin),
	).Scan(&eligible)
	if err != nil {
		if isInvalidTextRepresentation(err) {
			return false, nil
		}
		return false, fmt.Errorf("user repo: first admin eligibility: %w", err)
	}
	return eligible, nil
}

// scanUser reads one row in selectUsers' column order.
func scanUser(row pgx.Row) (User, error) {
	var (
		u     User
		roles []string
	)
	if err := row.Scan(
		&u.ID, &u.Subject, &u.Email, &u.Name, &u.CreatedAt, &u.LastLoginAt, &roles,
	); err != nil {
		return User{}, err
	}
	u.Roles = make([]Role, 0, len(roles))
	for _, r := range roles {
		u.Roles = append(u.Roles, Role(r))
	}
	return u, nil
}

// pgForeignKeyViolation is what Postgres reports when a grant names a user that
// does not exist.
const pgForeignKeyViolation = "23503"

// isForeignKeyViolation reports whether err is Postgres refusing a row whose
// reference points at nothing.
func isForeignKeyViolation(err error) bool {
	return hasSQLState(err, pgForeignKeyViolation)
}

// isInvalidTextRepresentation reports whether err is Postgres refusing to parse a
// value as its column's type. Every caller here reaches it the same way — an id
// from a URL path that is not a UUID — which is a request for something that does
// not exist rather than a fault worth a 500.
func isInvalidTextRepresentation(err error) bool {
	return hasSQLState(err, pgInvalidTextRepresentation)
}

func hasSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
