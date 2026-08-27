package agentmemory

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cursors are keyset, not offset.
//
// An offset would be wrong here in a way that shows up constantly rather than
// rarely: the thread listing is ordered by last activity, and writing to a
// conversation is exactly what moves it to the top. So between two pages of an
// offset listing, an active conversation shifts everything down and the caller
// silently skips a row. A keyset cursor names where it stopped instead, and the
// listing index is ordered to match.
//
// The encoding is opaque on purpose — base64 of an internal pair — so callers
// treat it as a token to hand back rather than something to construct.

// encodeThreadCursor names the last thread of a page by its sort key.
func encodeThreadCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

// decodeThreadCursor reads a thread cursor back. An empty cursor is the start of
// the listing, and returns a nil time so the query's IS NULL branch matches
// everything.
func decodeThreadCursor(cursor string) (*time.Time, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", fmt.Errorf("%w: cursor is not decodable", ErrInvalidRef)
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, "", fmt.Errorf("%w: cursor is malformed", ErrInvalidRef)
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, "", fmt.Errorf("%w: cursor timestamp is malformed", ErrInvalidRef)
	}
	return &parsed, id, nil
}

// encodeSeqCursor names the last turn of a transcript page.
func encodeSeqCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

// decodeSeqCursor reads a transcript cursor back. Zero is the start.
func decodeSeqCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("%w: cursor is not decodable", ErrInvalidRef)
	}
	seq, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: cursor is malformed", ErrInvalidRef)
	}
	return seq, nil
}
