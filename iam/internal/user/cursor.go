package user

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Paging position, as a caller carries it between requests.
//
// The directory is ordered by (created_at, id), so a position is exactly that
// pair. It is encoded rather than exposed as two parameters because it is this
// module's bookkeeping and not something a caller composes: a client that builds
// its own would be depending on the sort order, which is the thing most likely to
// change.

// position is where a page resumes from. The zero value is the beginning.
type position struct {
	createdAt *time.Time
	id        *string
}

// encodeCursor renders the position after the row (createdAt, id).
func encodeCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(createdAt.UTC().Format(time.RFC3339Nano) + "|" + id),
	)
}

// decodeCursor reads a cursor produced by encodeCursor. An empty cursor is the
// beginning; anything unreadable is the caller's mistake and says so, rather
// than silently restarting the listing from the top.
func decodeCursor(cursor string) (position, error) {
	if cursor == "" {
		return position{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return position{}, fmt.Errorf("%w: the cursor is not readable", ErrInvalid)
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return position{}, fmt.Errorf("%w: the cursor is not readable", ErrInvalid)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return position{}, fmt.Errorf("%w: the cursor is not readable", ErrInvalid)
	}
	// The id is bound to a uuid column, so one that is not a UUID reaches Postgres
	// and comes back as a type error — a 500 for a value the caller supplied.
	// Refused here, where every caller-supplied cursor already passes.
	if _, err := uuid.Parse(id); err != nil {
		return position{}, fmt.Errorf("%w: the cursor is not readable", ErrInvalid)
	}
	return position{createdAt: &createdAt, id: &id}, nil
}

// escapeLike neutralises the wildcards in a substring search, so an address
// containing one matches itself rather than everything.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
