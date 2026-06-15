package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// responseCache is a small in-memory TTL cache of GET responses, keyed by final
// URL. It is safe for concurrent use. Entries store the status, headers, and
// (already buffered) body; each lookup synthesizes a fresh *http.Response with a
// new body reader so callers can always read and close it.
type responseCache struct {
	ttl        time.Duration
	maxEntries int

	mu      sync.Mutex
	entries map[string]cacheEntry
}

// cacheEntry is a buffered response plus its expiry.
type cacheEntry struct {
	status  int
	header  http.Header
	body    []byte
	expires time.Time
}

func newResponseCache(ttl time.Duration, maxEntries int) *responseCache {
	return &responseCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[string]cacheEntry),
	}
}

// get returns a synthesized response for key when a live (unexpired) entry
// exists. Expired entries are dropped on access.
func (c *responseCache) get(key string) (*http.Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.response(), true
}

// store reads resp's body (bounded by maxBytes), records it under key, and
// returns a fresh response carrying the same buffered body for the caller. It
// closes resp.Body. The original response must not be used afterward.
func (c *responseCache) store(key string, resp *http.Response, maxBytes int64) (*http.Response, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	entry := cacheEntry{
		status:  resp.StatusCode,
		header:  resp.Header.Clone(),
		body:    body,
		expires: time.Now().Add(c.ttl),
	}

	c.mu.Lock()
	c.evictIfFull()
	c.entries[key] = entry
	c.mu.Unlock()

	return entry.response(), nil
}

// evictIfFull keeps the cache within maxEntries. It first drops expired entries,
// then, if still at capacity, removes one arbitrary entry. Callers hold c.mu.
func (c *responseCache) evictIfFull() {
	if len(c.entries) < c.maxEntries {
		return
	}
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) < c.maxEntries {
		return
	}
	for k := range c.entries {
		delete(c.entries, k)
		break
	}
}

// response builds a fresh *http.Response from the entry. Each call gets its own
// body reader and a copy of the headers, so concurrent callers do not share
// mutable state.
func (e cacheEntry) response() *http.Response {
	return &http.Response{
		StatusCode:    e.status,
		Status:        fmt.Sprintf("%d %s", e.status, http.StatusText(e.status)),
		Header:        e.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(e.body)),
		ContentLength: int64(len(e.body)),
	}
}

// limitedBody wraps a response body so reads stop after a byte limit while still
// closing the underlying body. It guards downstream blocks from an unbounded
// upstream response.
type limitedBody struct {
	reader io.Reader
	closer io.Closer
}

func newLimitedBody(rc io.ReadCloser, limit int64) io.ReadCloser {
	return &limitedBody{reader: io.LimitReader(rc, limit), closer: rc}
}

func (l *limitedBody) Read(p []byte) (int, error) {
	//nolint:wrapcheck // io.Reader pass-through must return io.EOF unwrapped so io.ReadAll terminates
	return l.reader.Read(p)
}

func (l *limitedBody) Close() error {
	//nolint:wrapcheck // io.Closer pass-through; wrapping would fabricate an error from a nil result
	return l.closer.Close()
}
