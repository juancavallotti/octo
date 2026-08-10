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

// fixedNow is the clock the default window is measured against, so the test can
// assert exact bounds instead of a range.
var fixedNow = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// Record ids are uuids in the table, and the route rejects anything else — so a
// fixture that used a friendlier name would exercise the malformed path while
// claiming to test the absent one.
const (
	recordID      = "aaaaaaaa-1111-2222-3333-444444444444"
	absentID      = "bbbbbbbb-1111-2222-3333-444444444444"
	deploymentID  = "cccccccc-1111-2222-3333-444444444444"
	integrationID = "dddddddd-1111-2222-3333-444444444444"
)

// fakeTraceQuerier records what it was asked for and returns canned rows.
type fakeTraceQuerier struct {
	gotFrom, gotTo time.Time
	gotFilter      repo.TraceFilter
	called         bool
	apps           []repo.TraceApp
	traces         []repo.TraceListRow

	summary      repo.TraceListRow
	summaryFound bool
	records      []repo.TraceRecordRow
	truncated    bool
	record       repo.TraceRecordRow
	recordFound  bool

	gotTraceID    string
	gotRecordID   string
	gotWithBodies bool

	err error
}

func (f *fakeTraceQuerier) Trace(_ context.Context, traceID string) (repo.TraceListRow, bool, error) {
	f.called = true
	f.gotTraceID = traceID
	return f.summary, f.summaryFound, f.err
}

func (f *fakeTraceQuerier) Records(_ context.Context, traceID string, withBodies bool) ([]repo.TraceRecordRow, bool, error) {
	f.gotTraceID = traceID
	f.gotWithBodies = withBodies
	return f.records, f.truncated, f.err
}

func (f *fakeTraceQuerier) Record(_ context.Context, traceID, id string) (repo.TraceRecordRow, bool, error) {
	f.called = true
	f.gotTraceID, f.gotRecordID = traceID, id
	return f.record, f.recordFound, f.err
}

func (f *fakeTraceQuerier) Apps(_ context.Context, from, to time.Time) ([]repo.TraceApp, error) {
	f.called = true
	f.gotFrom, f.gotTo = from, to
	return f.apps, f.err
}

func (f *fakeTraceQuerier) List(_ context.Context, filter repo.TraceFilter) ([]repo.TraceListRow, error) {
	f.called = true
	f.gotFilter = filter
	return f.traces, f.err
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

func decodeTraces(t *testing.T, rec *httptest.ResponseRecorder) traceListResponse {
	t.Helper()
	var got traceListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

// TestTraceListParsesEveryFilter checks each query parameter reaches the query it
// names. A filter that silently never arrives returns a plausible page of the
// wrong traces.
func TestTraceListParsesEveryFilter(t *testing.T) {
	q := &fakeTraceQuerier{}
	rec := doTraces(t, q, "/traces?deploymentId="+deploymentID+"&integrationId="+integrationID+
		"&appName=checkout&appVersion=v7"+
		"&flow=orders&status=failed&status=dropped&minDurationMs=250&hasLlm=true&q=boom"+
		"&from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z&limit=50")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}

	f := q.gotFilter
	if f.DeploymentID != deploymentID || f.IntegrationID != integrationID {
		t.Errorf("ids = %q/%q, want %s/%s", f.DeploymentID, f.IntegrationID, deploymentID, integrationID)
	}
	if f.AppName != "checkout" || f.AppVersion != "v7" || f.Flow != "orders" {
		t.Errorf("app filter = %q/%q/%q, want checkout/v7/orders", f.AppName, f.AppVersion, f.Flow)
	}
	if len(f.Statuses) != 2 || f.Statuses[0] != "failed" || f.Statuses[1] != "dropped" {
		t.Errorf("statuses = %v, want [failed dropped]", f.Statuses)
	}
	if f.MinDuration != 250*time.Millisecond {
		t.Errorf("min duration = %s, want 250ms", f.MinDuration)
	}
	if !f.HasLLM {
		t.Error("hasLlm=true did not reach the filter")
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

// TestTraceListCursorRoundTrips checks the cursor a page hands out is the one the
// next request can resume from, to the nanosecond.
//
// Rounding it would resume in the wrong place: the database orders on the full
// timestamp, so a cursor truncated to the second sits before every trace that
// started later in that same second.
func TestTraceListCursorRoundTrips(t *testing.T) {
	startedAt := time.Date(2026, 8, 9, 12, 0, 0, 123456789, time.UTC)
	q := &fakeTraceQuerier{traces: []repo.TraceListRow{
		{TraceID: "trace-a", StartedAt: startedAt.Add(time.Second)},
		{TraceID: "trace-b", StartedAt: startedAt},
	}}

	rec := doTraces(t, q, "/traces?limit=2")
	cursor := decodeTraces(t, rec).NextBefore
	if cursor == "" {
		t.Fatal("a full page carried no cursor")
	}

	next := &fakeTraceQuerier{}
	rec = doTraces(t, next, "/traces?limit=2&before="+url.QueryEscape(cursor))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if next.gotFilter.Before == nil {
		t.Fatal("the returned cursor did not parse back into a filter")
	}
	if !next.gotFilter.Before.StartedAt.Equal(startedAt) {
		t.Errorf("resumed at %s, want %s", next.gotFilter.Before.StartedAt.Format(time.RFC3339Nano),
			startedAt.Format(time.RFC3339Nano))
	}
	if next.gotFilter.Before.TraceID != "trace-b" {
		t.Errorf("resumed from %q, want trace-b", next.gotFilter.Before.TraceID)
	}
}

// TestTraceListCursorOnlyOnAFullPage checks a short page ends the walk. A cursor
// on the last page makes a client ask for one more, which is harmless — but a
// client that stops only when the cursor is absent would never stop.
func TestTraceListCursorOnlyOnAFullPage(t *testing.T) {
	q := &fakeTraceQuerier{traces: []repo.TraceListRow{{TraceID: "trace-a", StartedAt: fixedNow}}}
	if got := decodeTraces(t, doTraces(t, q, "/traces?limit=2")).NextBefore; got != "" {
		t.Errorf("short page carried cursor %q, want none", got)
	}
}

// TestTraceListCursorSplitsAtTheFirstSeparator checks a trace id containing the
// separator survives the round trip, since the split is what decides where the id
// begins.
func TestTraceListCursorSplitsAtTheFirstSeparator(t *testing.T) {
	q := &fakeTraceQuerier{}
	cursor := fixedNow.Format(time.RFC3339Nano) + "|odd|id"
	rec := doTraces(t, q, "/traces?before="+url.QueryEscape(cursor))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if q.gotFilter.Before == nil || q.gotFilter.Before.TraceID != "odd|id" {
		t.Errorf("trace id = %+v, want odd|id", q.gotFilter.Before)
	}
}

// TestTraceListRejectsBadInput keeps a request that can only mislead from
// reaching the database.
func TestTraceListRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"unknown status":        "/traces?status=broken",
		"cursor with no id":     "/traces?before=" + url.QueryEscape(fixedNow.Format(time.RFC3339Nano)),
		"cursor with no time":   "/traces?before=" + url.QueryEscape("yesterday|trace-a"),
		"negative min duration": "/traces?minDurationMs=-5",
		"non-numeric duration":  "/traces?minDurationMs=quick",
		"non-numeric limit":     "/traces?limit=many",
		// The window matches nothing, and an empty page reads like "this app ran
		// no traces" — the answer a swapped pair is most likely mistaken for.
		"window runs backwards": "/traces?from=2026-06-28T11:00:00Z&to=2026-06-28T10:00:00Z",
	}

	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			q := &fakeTraceQuerier{}
			rec := doTraces(t, q, target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
			if q.called {
				t.Error("a rejected request still reached the querier")
			}
		})
	}
}

// TestTraceListAcceptsEveryWindowThatIsNotBackwards guards the check above from
// growing teeth it should not have. Unlike /traces/apps, this route treats both
// bounds as optional — one-sided and unbounded are ordinary requests, and an
// instant is a window a caller can legitimately ask for.
func TestTraceListAcceptsEveryWindowThatIsNotBackwards(t *testing.T) {
	for name, target := range map[string]string{
		"no bounds":  "/traces",
		"from only":  "/traces?from=2026-06-28T10:00:00Z",
		"to only":    "/traces?to=2026-06-28T11:00:00Z",
		"an instant": "/traces?from=2026-06-28T10:00:00Z&to=2026-06-28T10:00:00Z",
		"in order":   "/traces?from=2026-06-28T10:00:00Z&to=2026-06-28T11:00:00Z",
	} {
		t.Run(name, func(t *testing.T) {
			q := &fakeTraceQuerier{}
			if rec := doTraces(t, q, target); rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
			}
		})
	}
}

// TestTraceListDefaultsAndClampsLimit keeps one request from being able to ask
// for the whole table.
func TestTraceListDefaultsAndClampsLimit(t *testing.T) {
	cases := map[string]int{
		"/traces":            defaultLimit,
		"/traces?limit=0":    1,
		"/traces?limit=9999": maxLimit,
	}
	for target, want := range cases {
		q := &fakeTraceQuerier{}
		if rec := doTraces(t, q, target); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, rec.Code)
		}
		if q.gotFilter.Limit != want {
			t.Errorf("%s: limit = %d, want %d", target, q.gotFilter.Limit, want)
		}
	}
}

// TestTraceListReturnsAnEmptyListNotNull keeps the response shape stable when
// nothing matched.
func TestTraceListReturnsAnEmptyListNotNull(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{}, "/traces")
	var raw struct {
		Items *[]repo.TraceListRow `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.Items == nil {
		t.Fatal("items was null, want []")
	}
}

// TestTraceListFailsClosedOnAQueryError checks a broken query is not served as an
// empty page.
func TestTraceListFailsClosedOnAQueryError(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{err: context.DeadlineExceeded}, "/traces")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (body %s)", rec.Code, rec.Body)
	}
}

// TestTraceListLeavesOptionalFlagsOff checks an absent flag stays absent. A list
// silently narrowed to model calls would show a handful of traces and look like
// an app that barely runs.
func TestTraceListLeavesOptionalFlagsOff(t *testing.T) {
	for _, target := range []string{"/traces", "/traces?hasLlm=false", "/traces?hasLlm="} {
		q := &fakeTraceQuerier{}
		if rec := doTraces(t, q, target); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, rec.Code)
		}
		if q.gotFilter.HasLLM {
			t.Errorf("%s: hasLlm was on without being asked for", target)
		}
	}
}

func decodeDetail(t *testing.T, rec *httptest.ResponseRecorder) traceDetailResponse {
	t.Helper()
	var got traceDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

// foundTrace is a querier that answers for one trace.
func foundTrace(records ...repo.TraceRecordRow) *fakeTraceQuerier {
	return &fakeTraceQuerier{
		summary:      repo.TraceListRow{TraceID: "trace-a", Records: len(records)},
		summaryFound: true,
		records:      records,
	}
}

// TestTraceDetailServesTheStoredSummary checks the panel is filled from the
// rollup rather than recomputed. The list and the detail must report the same
// numbers because they read the same row, not because two implementations agree.
func TestTraceDetailServesTheStoredSummary(t *testing.T) {
	q := foundTrace(repo.TraceRecordRow{ID: "rec-1", Seq: 1, Kind: "flow.started"})
	q.summary = repo.TraceListRow{TraceID: "trace-a", Records: 1, LLMCalls: 2, CostUSD: 0.5, UnpricedCalls: 1}

	rec := doTraces(t, q, "/traces/trace-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if q.gotTraceID != "trace-a" {
		t.Errorf("queried %q, want trace-a", q.gotTraceID)
	}

	got := decodeDetail(t, rec)
	if got.Summary.CostUSD != 0.5 || got.Summary.UnpricedCalls != 1 || got.Summary.LLMCalls != 2 {
		t.Errorf("summary = %+v, want the stored rollup", got.Summary)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "rec-1" {
		t.Errorf("items = %+v, want the one record", got.Items)
	}
	if got.Truncated {
		t.Error("a one-record trace reported as truncated")
	}
}

// TestTraceDetailBodiesParameter checks the flag reaches the query, and defaults
// to on so a plain fetch is complete.
func TestTraceDetailBodiesParameter(t *testing.T) {
	cases := map[string]bool{
		"/traces/trace-a":              true,
		"/traces/trace-a?bodies=1":     true,
		"/traces/trace-a?bodies=0":     false,
		"/traces/trace-a?bodies=false": false,
	}
	for target, want := range cases {
		q := foundTrace()
		if rec := doTraces(t, q, target); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, rec.Code)
		}
		if q.gotWithBodies != want {
			t.Errorf("%s: withBodies = %v, want %v", target, q.gotWithBodies, want)
		}
	}
}

// TestTraceDetailReportsTruncation checks a cut record set says so. A waterfall
// drawn from one is wrong in a way nothing in the picture reveals.
func TestTraceDetailReportsTruncation(t *testing.T) {
	q := foundTrace(repo.TraceRecordRow{ID: "rec-1"})
	q.truncated = true

	if got := decodeDetail(t, doTraces(t, q, "/traces/trace-a")); !got.Truncated {
		t.Error("truncated record set was served without the flag")
	}
}

// TestTraceDetailMissingTraceIs404 keeps "no such trace" from reading as a trace
// that happened to have no records.
func TestTraceDetailMissingTraceIs404(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{}, "/traces/nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body %s)", rec.Code, rec.Body)
	}
}

// TestTraceDetailFailsClosedOnAQueryError separates a broken query from an
// absent trace: one is a 500 and the other a 404, and serving the first as the
// second would report a database outage as deleted data.
func TestTraceDetailFailsClosedOnAQueryError(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{err: context.DeadlineExceeded}, "/traces/trace-a")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (body %s)", rec.Code, rec.Body)
	}
}

// TestTraceRecordServesOnePayload checks the click-to-inspect route, including
// that it carries both identifiers into the query.
func TestTraceRecordServesOnePayload(t *testing.T) {
	q := &fakeTraceQuerier{
		recordFound: true,
		record:      repo.TraceRecordRow{ID: recordID, TraceID: "trace-a", Body: json.RawMessage(`{"order":7}`)},
	}

	rec := doTraces(t, q, "/traces/trace-a/records/"+recordID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if q.gotTraceID != "trace-a" || q.gotRecordID != recordID {
		t.Errorf("queried %q/%q, want trace-a/%s", q.gotTraceID, q.gotRecordID, recordID)
	}

	var got repo.TraceRecordRow
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got.Body) != `{"order":7}` {
		t.Errorf("body = %s, want the stored document", got.Body)
	}
}

// TestTraceRecordMissingIs404 covers both a record that does not exist and one
// asked for under the wrong trace, which the store reports the same way.
func TestTraceRecordMissingIs404(t *testing.T) {
	rec := doTraces(t, &fakeTraceQuerier{}, "/traces/trace-a/records/"+absentID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body %s)", rec.Code, rec.Body)
	}
}

// TestTraceRecordKeepsAnUnknownCostNull is the last place a NULL cost could
// become a zero: the JSON a client actually reads.
func TestTraceRecordKeepsAnUnknownCostNull(t *testing.T) {
	q := &fakeTraceQuerier{
		recordFound: true,
		record: repo.TraceRecordRow{
			ID: recordID, TraceID: "trace-a", Kind: "llm.turn",
			Model: "unknown-model", CostStatus: "unpriced_model",
		},
	}

	rec := doTraces(t, q, "/traces/trace-a/records/"+recordID)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"cost_usd", "input_tokens", "output_tokens", "cached_tokens"} {
		if string(raw[field]) != "null" {
			t.Errorf("%s = %s, want null", field, raw[field])
		}
	}
}

// TestTraceAppsRouteIsNotReadAsATraceId checks the literal path still wins over
// the {traceId} pattern now that both are registered.
func TestTraceAppsRouteIsNotReadAsATraceId(t *testing.T) {
	q := &fakeTraceQuerier{}
	if rec := doTraces(t, q, "/traces/apps"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if q.gotTraceID != "" {
		t.Errorf("/traces/apps was served as trace %q", q.gotTraceID)
	}
}

// TestTraceListRejectsAMalformedUUID checks a bad identifier is the caller's
// error, not the server's. These reach the query as ::uuid casts, so without a
// check Postgres rejects them and the response blames the server for a value the
// caller typed.
func TestTraceListRejectsAMalformedUUID(t *testing.T) {
	for _, target := range []string{"/traces?deploymentId=not-a-uuid", "/traces?integrationId=12345"} {
		q := &fakeTraceQuerier{}
		rec := doTraces(t, q, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", target, rec.Code, rec.Body)
		}
		if q.called {
			t.Errorf("%s: a malformed uuid still reached the querier", target)
		}
	}
}

// TestTraceListAcceptsAWellFormedUUID keeps the check from rejecting the values
// it exists to let through.
func TestTraceListAcceptsAWellFormedUUID(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"
	q := &fakeTraceQuerier{}
	if rec := doTraces(t, q, "/traces?deploymentId="+id+"&integrationId="+id); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if q.gotFilter.DeploymentID != id {
		t.Errorf("deployment = %q, want %q", q.gotFilter.DeploymentID, id)
	}
}

// TestTraceRecordMalformedIdIs404 answers a malformed address the same way as an
// absent one: both name no record, and answering alike keeps the route from
// confirming which ids exist.
func TestTraceRecordMalformedIdIs404(t *testing.T) {
	q := &fakeTraceQuerier{recordFound: true}
	rec := doTraces(t, q, "/traces/trace-a/records/not-a-uuid")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body %s)", rec.Code, rec.Body)
	}
	if q.called {
		t.Error("a malformed record id still reached the querier")
	}
}
