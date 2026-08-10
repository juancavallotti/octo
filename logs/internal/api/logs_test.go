package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/juancavallotti/octo/logs/internal/repo"
)

// fakeLogQuerier records the filter it was called with and returns canned rows.
type fakeLogQuerier struct {
	gotFilter repo.LogFilter
	rows      []repo.LogRow
	err       error
}

func (f *fakeLogQuerier) Query(_ context.Context, filter repo.LogFilter) ([]repo.LogRow, error) {
	f.gotFilter = filter
	return f.rows, f.err
}

func do(t *testing.T, q LogQuerier, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewLogsHandler(q).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestListParsesFiltersIntoQuery(t *testing.T) {
	q := &fakeLogQuerier{}
	rec := do(t, q, "/logs?deploymentId=dep-1&appName=checkout&appVersion=v2&level=ERROR&level=WARN&q=boom&from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z&limit=50")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}

	f := q.gotFilter
	if f.DeploymentID != "dep-1" {
		t.Errorf("deploymentId = %q, want dep-1", f.DeploymentID)
	}
	if f.AppName != "checkout" || f.AppVersion != "v2" {
		t.Errorf("app filter = %q/%q, want checkout/v2", f.AppName, f.AppVersion)
	}
	if len(f.Levels) != 2 || f.Levels[0] != "ERROR" || f.Levels[1] != "WARN" {
		t.Errorf("levels = %v, want [ERROR WARN]", f.Levels)
	}
	if f.Search != "boom" {
		t.Errorf("search = %q, want boom", f.Search)
	}
	if f.From == nil || f.To == nil {
		t.Fatalf("from/to not parsed: %+v", f)
	}
	if f.Limit != 50 {
		t.Errorf("limit = %d, want 50", f.Limit)
	}
}

func TestListDefaultsAndClampsLimit(t *testing.T) {
	cases := map[string]int{
		"/logs":            defaultLimit,
		"/logs?limit=0":    1,
		"/logs?limit=9999": maxLimit,
	}
	for target, want := range cases {
		q := &fakeLogQuerier{}
		if rec := do(t, q, target); rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", target, rec.Code)
		}
		if q.gotFilter.Limit != want {
			t.Errorf("%s: limit = %d, want %d", target, q.gotFilter.Limit, want)
		}
	}
}

func TestListRejectsBadParams(t *testing.T) {
	for _, target := range []string{"/logs?limit=abc", "/logs?from=not-a-time"} {
		if rec := do(t, &fakeLogQuerier{}, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

func TestListSetsNextBeforeOnFullPage(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	q := &fakeLogQuerier{rows: []repo.LogRow{
		{ID: "a", Time: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		{ID: "b", Time: oldest},
	}}
	rec := do(t, q, "/logs?limit=2") // len(rows) == limit -> more may exist

	var resp logListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The pair, not the timestamp: the id is what lets the next page resume after
	// this exact row rather than after this second.
	if want := oldest.Format(time.RFC3339Nano) + "|b"; resp.NextBefore != want {
		t.Errorf("next_before = %q, want %q", resp.NextBefore, want)
	}
	if len(resp.Items) != 2 {
		t.Errorf("items = %d, want 2", len(resp.Items))
	}
}

func TestListOmitsNextBeforeOnPartialPage(t *testing.T) {
	q := &fakeLogQuerier{rows: []repo.LogRow{{ID: "a", Time: time.Now()}}}
	rec := do(t, q, "/logs?limit=10") // fewer than limit -> last page

	var resp logListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NextBefore != "" {
		t.Errorf("next_before = %q, want empty on a partial page", resp.NextBefore)
	}
}

// TestListCursorRoundTrips walks the cursor back in as ?before= and checks the
// filter it produces, which is the half a client actually depends on.
func TestListCursorRoundTrips(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 9, 0, 0, 123456789, time.UTC)
	first := &fakeLogQuerier{rows: []repo.LogRow{
		{ID: "a", Time: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		{ID: "b", Time: oldest},
	}}
	var page logListResponse
	if err := json.Unmarshal(do(t, first, "/logs?limit=2").Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}

	next := &fakeLogQuerier{}
	rec := do(t, next, "/logs?limit=2&before="+url.QueryEscape(page.NextBefore))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if next.gotFilter.Before == nil {
		t.Fatal("before was not parsed into the filter")
	}
	// Nanoseconds survive: rows a microsecond apart are ordered on the full
	// timestamp, so a cursor rounded to the second resumes in the wrong place.
	if got := next.gotFilter.Before; !got.Time.Equal(oldest) || got.ID != "b" {
		t.Errorf("before = %v/%q, want %v/b", got.Time, got.ID, oldest)
	}
}

// TestListRejectsBadCursor: a malformed cursor is a 400, not a silent first page.
// Serving the newest rows again in the middle of a scroll would look like data
// duplicating itself.
func TestListRejectsBadCursor(t *testing.T) {
	for _, raw := range []string{
		"not-a-cursor",
		"2026-01-01T00:00:00Z",  // timestamp only, the old wire format
		"2026-01-01T00:00:00Z|", // empty id
		"nonsense|some-id",
	} {
		rec := do(t, &fakeLogQuerier{}, "/logs?before="+url.QueryEscape(raw))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("before=%q gave status %d, want 400", raw, rec.Code)
		}
	}
}

// A log id may contain the separator in principle; the first one wins, so the id
// survives intact.
func TestListCursorSplitsAtTheFirstSeparator(t *testing.T) {
	q := &fakeLogQuerier{}
	ts := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	rec := do(t, q, "/logs?before="+url.QueryEscape(ts.Format(time.RFC3339Nano)+"|a|b"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if got := q.gotFilter.Before; got == nil || got.ID != "a|b" {
		t.Errorf("cursor id = %v, want a|b", got)
	}
}

func TestListReturnsEmptyArrayNotNull(t *testing.T) {
	rec := do(t, &fakeLogQuerier{rows: nil}, "/logs")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["items"]) != "[]" {
		t.Errorf("items = %s, want []", raw["items"])
	}
}
