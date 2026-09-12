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

	// pgUniqueViolation is what Postgres reports when a row would duplicate a
	// unique key — creating a user whose subject already has an account, here.
	pgUniqueViolation = "23505"
)

// Repo persists users and their role grants to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Admit resolves the caller of a sign-in: it refreshes the user identified by
// subject, or — on an installation that has no administrator — creates them and
// makes them one.
//
// All of it in one transaction under one lock, and that is the whole point of the
// method existing. Done as separate steps the two checks are both
// time-of-check-to-time-of-use races:
//
//   - two unprovisioned strangers signing in at once could both observe "no
//     administrator", and although only one would end up with the role, the other
//     would be left with a real account — permanently past an allowlist that was
//     supposed to refuse them;
//   - and the account that gets the role has to be the same one the check was
//     made about, or a fresh installation can end up administered by nobody.
//
// The lock is the same one every decision about "does this platform have an
// administrator" is taken under, because they are all the same invariant.
//
// The second return reports whether this call created the row, which the caller
// has no other way to know and which reads better in a log than a guess.
func (r *Repo) Admit(ctx context.Context, subject, email, name string) (User, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return User{}, false, fmt.Errorf("user repo: admit: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	// Transaction-scoped, so it is released by the commit or the rollback above
	// and cannot be leaked by an early return.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey); err != nil {
		return User{}, false, fmt.Errorf("user repo: admit: lock: %w", err)
	}

	var (
		id      string
		created bool
	)
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE subject = $1`, subject).Scan(&id)
	switch {
	case err == nil:
		if _, err := tx.Exec(ctx,
			`UPDATE users SET email = $2, name = $3, last_login_at = now() WHERE id = $1`,
			id, email, name,
		); err != nil {
			return User{}, false, fmt.Errorf("user repo: admit: refresh: %w", err)
		}
	case errors.Is(err, pgx.ErrNoRows):
		if id, err = r.admitNewcomer(ctx, tx, subject, email, name); err != nil {
			return User{}, false, err
		}
		created = true
	default:
		return User{}, false, fmt.Errorf("user repo: admit: look up subject: %w", err)
	}

	u, err := scanUser(tx.QueryRow(ctx, selectUsers+` WHERE u.id = $1 GROUP BY u.id`, id))
	if err != nil {
		return User{}, false, fmt.Errorf("user repo: admit: read back: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, false, fmt.Errorf("user repo: admit: commit: %w", err)
	}
	return u, created, nil
}

// admitNewcomer creates the account for somebody with no row yet, which is only
// allowed while the installation has no administrator — see Admit. It runs inside
// that method's transaction and under its lock.
//
// The grant is part of the same statement sequence rather than a later call,
// because "the first user is an administrator" is only true if nothing can happen
// between deciding it and doing it.
func (r *Repo) admitNewcomer(
	ctx context.Context, tx pgx.Tx, subject, email, name string,
) (string, error) {
	var hasAdmin bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM user_roles WHERE role = $1)`, string(RoleAdmin),
	).Scan(&hasAdmin); err != nil {
		return "", fmt.Errorf("user repo: admit: look for an administrator: %w", err)
	}
	if hasAdmin {
		return "", ErrNotProvisioned
	}

	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (subject, email, name) VALUES ($1, $2, $3) RETURNING id`,
		subject, email, name,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("user repo: admit: create: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_roles (user_id, role) VALUES ($1, $2)
		 ON CONFLICT (user_id, role) DO NOTHING`,
		id, string(RoleAdmin),
	); err != nil {
		return "", fmt.Errorf("user repo: admit: grant the first administrator: %w", err)
	}
	return id, nil
}

// Create provisions a user an administrator named, rather than one who signed in.
//
// It is how somebody is let in before their first sign-in: this platform admits
// only provisioned users (see Service.SignIn), so an account has to exist here
// before the person it belongs to can get past the door. The subject is the one
// the identity provider will present, which an administrator reads out of their
// provider's own console — there is no way for us to discover it, and guessing
// at it from an email address would let one person be admitted as another.
//
// A subject that already exists is ErrConflict rather than an update: the caller
// asked to create somebody, and quietly rewriting an existing account's email
// would be a different and much worse thing to do.
func (r *Repo) Create(ctx context.Context, subject, email, name string) (User, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (subject, email, name)
		 VALUES ($1, $2, $3)
		 RETURNING id, subject, email, name, created_at, last_login_at`,
		subject, email, name,
	)
	var u User
	if err := row.Scan(&u.ID, &u.Subject, &u.Email, &u.Name, &u.CreatedAt, &u.LastLoginAt); err != nil {
		if hasSQLState(err, pgUniqueViolation) {
			return User{}, ErrConflict
		}
		return User{}, fmt.Errorf("user repo: create: %w", err)
	}
	return u, nil
}

// Update rewrites the profile fields an administrator may correct. The subject is
// not among them: it is what the row is keyed by and what the identity provider
// will present, so changing it would silently point an account at a different
// person.
func (r *Repo) Update(ctx context.Context, id, email, name string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET email = $2, name = $3 WHERE id = $1`, id, email, name)
	if err != nil {
		if isInvalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user repo: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a user. Their role grants and API keys go with them (ON DELETE
// CASCADE); what they authored does not, because those columns are ON DELETE SET
// NULL — an integration outlives whoever created it, and losing the integration
// along with the person who left would be a far worse outcome than losing the
// attribution.
func (r *Repo) Delete(ctx context.Context, id string) error {
	return r.underAdminLock(ctx, "delete", func(tx pgx.Tx) error {
		if err := lastAdminCheck(ctx, tx, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		if err != nil {
			if isInvalidTextRepresentation(err) {
				return ErrNotFound
			}
			return fmt.Errorf("user repo: delete: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
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
		// Two foreign keys can refuse this row, and they mean opposite things: the
		// target does not exist, or the caller does not. Reported apart, because
		// "user not found" about a user the administrator is looking at sends them
		// hunting for the wrong problem.
		if violated(err, grantedByConstraint) {
			return ErrGranterGone
		}
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
	if revoked != RoleAdmin {
		return r.revoke(ctx, r.pool, userID, revoked)
	}
	// Taking the administrator role away is a decision about whether this platform
	// still has one, so it is made under the lock every such decision is made
	// under, in the same transaction as the write it authorises.
	return r.underAdminLock(ctx, "revoke", func(tx pgx.Tx) error {
		if err := lastAdminCheck(ctx, tx, userID); err != nil {
			return err
		}
		return r.revoke(ctx, tx, userID, revoked)
	})
}

// revoke performs the deletion itself, against a pool or a transaction.
func (r *Repo) revoke(ctx context.Context, q execer, userID string, revoked Role) error {
	if _, err := q.Exec(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND role = $2`,
		userID, string(revoked),
	); err != nil {
		if isInvalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user repo: revoke: %w", err)
	}
	return nil
}

// underAdminLock runs fn in a transaction holding the advisory lock that every
// decision about "does this platform have an administrator" is taken under.
func (r *Repo) underAdminLock(ctx context.Context, what string, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("user repo: %s: begin: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey); err != nil {
		return fmt.Errorf("user repo: %s: lock: %w", what, err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("user repo: %s: commit: %w", what, err)
	}
	return nil
}

// lastAdminCheck refuses an operation that would leave the platform with no
// administrator. Called inside underAdminLock, so what it observes cannot change
// before the write it guards.
//
// An installation with nobody able to administer it is not merely inconvenient:
// the bootstrap in Admit hands the role to the next stranger who signs in, so
// losing the last administrator is a way to give the platform away.
func lastAdminCheck(ctx context.Context, tx pgx.Tx, userID string) error {
	var last bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id = $1 AND role = $2)
		    AND (SELECT count(*) FROM user_roles WHERE role = $2) <= 1`,
		userID, string(RoleAdmin),
	).Scan(&last)
	if err != nil {
		if isInvalidTextRepresentation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("user repo: last administrator check: %w", err)
	}
	if last {
		return ErrLastAdmin
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

// execer is the write half of a pool or a transaction, so a statement can be run
// either on its own or inside one.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
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

// grantedByConstraint is the foreign key from a grant to the administrator who
// made it. Postgres names it by its table and column, and it is named here so the
// two keys on user_roles can be told apart.
const grantedByConstraint = "user_roles_granted_by_fkey"

// violated reports whether err is Postgres refusing a row because of the named
// constraint.
func violated(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == pgForeignKeyViolation && pgErr.ConstraintName == constraint
}

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
