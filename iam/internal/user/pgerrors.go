package user

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

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
