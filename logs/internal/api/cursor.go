package api

import (
	"strings"
	"time"
)

// cursorSeparator joins the two halves of a page cursor. An RFC3339 timestamp
// cannot contain one, so the split is unambiguous however the id is spelled.
const cursorSeparator = "|"

// encodeKeyset renders a page position as "<rfc3339nano>|<id>".
//
// Both list endpoints page by a composite key, because a timestamp alone is not
// one: rows sharing a timestamp at a page boundary are otherwise skipped or
// served twice depending on which side of it they land. The id breaks the tie,
// and it has to travel in the cursor for the next page to resume at the right
// row rather than the right second.
func encodeKeyset(ts time.Time, id string) string {
	return ts.Format(time.RFC3339Nano) + cursorSeparator + id
}

// decodeKeyset parses a cursor, reporting ok=false for an empty one so the first
// page needs no special case. A malformed cursor is an error, not an empty page:
// silently starting over would serve the client the newest rows again in the
// middle of a scroll.
//
// Nanosecond precision is carried through deliberately. Two rows a microsecond
// apart are ordered by the database on the full timestamp, so a cursor rounded to
// the second would resume in the wrong place.
func decodeKeyset(raw, label string) (ts time.Time, id string, ok bool, err error) {
	if raw == "" {
		return time.Time{}, "", false, nil
	}
	// Cut at the first separator, not the last: the timestamp cannot contain one,
	// so everything after the first is the id however it is spelled.
	stamp, id, found := strings.Cut(raw, cursorSeparator)
	if !found || id == "" {
		return time.Time{}, "", false, errInvalid("before must be <rfc3339>|<" + label + ">")
	}
	ts, parseErr := time.Parse(time.RFC3339Nano, stamp)
	if parseErr != nil {
		return time.Time{}, "", false, errInvalid("before must start with an RFC3339 timestamp: " + stamp)
	}
	return ts, id, true, nil
}
