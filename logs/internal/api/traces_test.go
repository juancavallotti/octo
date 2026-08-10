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

// fixedNow is the clock the default window is measured against, so the test can
// assert exact bounds instead of a range.
var fixedNow = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// fakeTraceQuerier records the window it was called with and returns canned rows.
type fakeTraceQuerier struct {
	gotFrom, gotTo time.Time
	called         bool
	apps           []repo.TraceApp
	err            error
}

func (f *fakeTraceQuerier) Apps(_ context.Context, from, to time.Time) ([]repo.TraceApp, error) {
	f.called = true
	f.gotFrom, f.gotTo = from, to
	return f.apps, f.err
}

func doTraces(t *testing.T, q TraceQuerier, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler := NewTracesHandler(q)
	handler.now = func() time.Time { return fixedNow }
	handler.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeApps(t *testing.T, rec *httptest.ResponseRecorder) appsResponse {
	t.Helper()
	var got appsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

// TestTraceAppsWindowDefaultsAndPartialBounds pins how a window is completed. A
// caller giving one end must not be silently widened to everything, since every
// count in the response is read against the window it was measured over.
func TestTraceAppsWindowDefaultsAndPartialBounds(t *testing.T) {
	noon := fixedNow
	morning := noon.Add(-4 * time.Hour)

	cases := []struct {
		name             string
		target           string
		wantFrom, wantTo time.Time
	}{
		{"neither bound", "/traces/apps", noon.Add(-defaultWindow), noon},
		{"from only", "/traces/apps?from=" + morning.Format(time.RFC3339), morning, noon},
		{"to only", "/traces/apps?to=" + morning.Format(time.RFC3339), morning.Add(-defaultWindow), morning},
		{
			"both bounds",
			"/traces/apps?from=" + morning.Format(time.RFC3339) + "&to=" + noon.Format(time.RFC3339),
			morning, noon,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeTraceQuerier{}
			rec := doTraces(t, q, tc.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
			}
			if !q.gotFrom.Equal(tc.wantFrom) || !q.gotTo.Equal(tc.wantTo) {
				t.Errorf("window = [%s, %s], want [%s, %s]",
					q.gotFrom.Format(time.RFC3339), q.gotTo.Format(time.RFC3339),
					tc.wantFrom.Format(time.RFC3339), tc.wantTo.Format(time.RFC3339))
			}

			// Echoed back, because the caller may not have chosen it.
			got := decodeApps(t, rec)
			if !got.From.Equal(tc.wantFrom) || !got.To.Equal(tc.wantTo) {
				t.Errorf("reported window = [%s, %s], want [%s, %s]",
					got.From, got.To, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

// TestTraceAppsRejectsAnUnusableWindow keeps a bad request from reaching the
// database as a query that can only ever return nothing.
func TestTraceAppsRejectsAnUnusableWindow(t *testing.T) {
	cases := map[string]string{
		"unparseable from": "/traces/apps?from=yesterday",
		"unparseable to":   "/traces/apps?to=12:00",
		"inverted window":  "/traces/apps?from=2026-08-09T12:00:00Z&to=2026-08-09T11:00:00Z",
	}

	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			q := &fakeTraceQuerier{}
			rec := doTraces(t, q, target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
			if q.called {
				t.Error("a rejected window still reached the querier")
			}
		})
	}
}

// TestTraceAppsReportsCostBesideWhatCouldNotBePriced checks the pair survives the
// wire. A total that arrived without its unpriced count reads as complete, and
// nothing downstream could tell it was a lower bound.
func TestTraceAppsReportsCostBesideWhatCouldNotBePriced(t *testing.T) {
	q := &fakeTraceQuerier{apps: []repo.TraceApp{{
		DeploymentID:   "dep-1",
		IntegrationID:  "int-1",
		AppName:        "checkout",
		AppVersion:     "v7",
		Traces:         12,
		Failed:         2,
		LastSeenAt:     fixedNow,
		CostUSD:        1.25,
		UnpricedCalls:  3,
		DroppedRecords: 9,
	}}}

	rec := doTraces(t, q, "/traces/apps")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}

	got := decodeApps(t, rec)
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.Items))
	}
	app := got.Items[0]
	if app.CostUSD != 1.25 || app.UnpricedCalls != 3 {
		t.Errorf("cost = %v with %d unpriced, want 1.25 with 3", app.CostUSD, app.UnpricedCalls)
	}
	if app.DroppedRecords != 9 {
		t.Errorf("dropped = %d, want 9", app.DroppedRecords)
	}
	if app.AppVersion != "v7" {
		t.Errorf("version = %q, want v7", app.AppVersion)
	}
}

// TestTraceAppsReturnsAnEmptyListNotNull keeps the response shape stable for a
// window with no activity: a client iterating items must not have to guard null.
func TestTraceAppsReturnsAnEmptyListNotNull(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{}, "/traces/apps")

	var raw struct {
		Items *[]repo.TraceApp `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.Items == nil {
		t.Fatal("items was null, want []")
	}
	if len(*raw.Items) != 0 {
		t.Errorf("items = %v, want empty", *raw.Items)
	}
}

// TestTraceAppsFailsClosedOnAQueryError checks a broken query is reported as one
// rather than as an app list that happens to be empty.
func TestTraceAppsFailsClosedOnAQueryError(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{err: context.DeadlineExceeded}, "/traces/apps")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (body %s)", rec.Code, rec.Body)
	}
}
