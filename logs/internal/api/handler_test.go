package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/juancavallotti/octo/logs/internal/repo"
)

// fakeQuerier records the filter it was called with and returns canned rows.
type fakeQuerier struct {
	gotFilter repo.LogFilter
	rows      []repo.LogRow
	err       error
}

func (f *fakeQuerier) Query(_ context.Context, filter repo.LogFilter) ([]repo.LogRow, error) {
	f.gotFilter = filter
	return f.rows, f.err
}

func do(t *testing.T, q Querier, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(q).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestListParsesFiltersIntoQuery(t *testing.T) {
	q := &fakeQuerier{}
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
		q := &fakeQuerier{}
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
		if rec := do(t, &fakeQuerier{}, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

func TestListSetsNextBeforeOnFullPage(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	q := &fakeQuerier{rows: []repo.LogRow{
		{ID: "a", Time: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		{ID: "b", Time: oldest},
	}}
	rec := do(t, q, "/logs?limit=2") // len(rows) == limit -> more may exist

	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NextBefore == nil || !resp.NextBefore.Equal(oldest) {
		t.Errorf("next_before = %v, want %v", resp.NextBefore, oldest)
	}
	if len(resp.Items) != 2 {
		t.Errorf("items = %d, want 2", len(resp.Items))
	}
}

func TestListOmitsNextBeforeOnPartialPage(t *testing.T) {
	q := &fakeQuerier{rows: []repo.LogRow{{ID: "a", Time: time.Now()}}}
	rec := do(t, q, "/logs?limit=10") // fewer than limit -> last page

	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NextBefore != nil {
		t.Errorf("next_before = %v, want nil on a partial page", resp.NextBefore)
	}
}

func TestListReturnsEmptyArrayNotNull(t *testing.T) {
	rec := do(t, &fakeQuerier{rows: nil}, "/logs")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["items"]) != "[]" {
		t.Errorf("items = %s, want []", raw["items"])
	}
}
