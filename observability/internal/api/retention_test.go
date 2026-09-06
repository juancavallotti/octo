package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/retention"
)

// fakeRetention records what it was asked to do and returns canned answers.
type fakeRetention struct {
	policy    retention.Policy
	gotUpdate *retention.Update
	updateErr error
	getErr    error
	result    retention.RunResult
	runErr    error
	runs      int
}

func (f *fakeRetention) Get(context.Context) (retention.Policy, error) {
	return f.policy, f.getErr
}

func (f *fakeRetention) Update(_ context.Context, u retention.Update) (retention.Policy, error) {
	f.gotUpdate = &u
	if f.updateErr != nil {
		return retention.Policy{}, f.updateErr
	}
	f.policy = retention.Policy{LogsDays: u.LogsDays, TracesDays: u.TracesDays}
	return f.policy, nil
}

func (f *fakeRetention) Run(context.Context) (retention.RunResult, error) {
	f.runs++
	return f.result, f.runErr
}

func doRetention(t *testing.T, svc RetentionService, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewRetentionHandler(svc).Register(mux)

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return out
}

func TestRetentionGetReportsThePolicy(t *testing.T) {
	when := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	svc := &fakeRetention{policy: retention.Policy{LogsDays: 30, TracesDays: 7, AlertsDays: 14, UpdatedAt: &when}}

	rec := doRetention(t, svc, http.MethodGet, "/settings/retention", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got := decodeBody[retentionPolicyResponse](t, rec)
	if got.LogsDays != 30 || got.TracesDays != 7 || got.AlertsDays != 14 {
		t.Fatalf("body = %+v", got)
	}
	if got.UpdatedAt == nil || !got.UpdatedAt.Equal(when) {
		t.Fatalf("updated_at = %v, want %v", got.UpdatedAt, when)
	}
}

// An unconfigured installation reads back as zeros. It is not an error state, and
// a client has to be able to tell it apart from one.
func TestRetentionGetOnAnUnconfiguredInstallIsZeros(t *testing.T) {
	rec := doRetention(t, &fakeRetention{}, http.MethodGet, "/settings/retention", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeBody[retentionPolicyResponse](t, rec)
	if got.LogsDays != 0 || got.TracesDays != 0 || got.UpdatedAt != nil {
		t.Fatalf("body = %+v, want an empty policy", got)
	}
}

func TestRetentionUpdatePassesEveryWindowThrough(t *testing.T) {
	svc := &fakeRetention{}

	rec := doRetention(t, svc, http.MethodPut, "/settings/retention",
		`{"logs_days":30,"traces_days":7,"alerts_days":14}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotUpdate == nil {
		t.Fatal("the service was not asked to update")
	}
	if svc.gotUpdate.LogsDays != 30 || svc.gotUpdate.TracesDays != 7 || svc.gotUpdate.AlertsDays != 14 {
		t.Fatalf("update = %+v", *svc.gotUpdate)
	}
}

func TestRetentionUpdateRejectsAnImpossibleWindow(t *testing.T) {
	svc := &fakeRetention{updateErr: retention.ErrInvalidDays}

	rec := doRetention(t, svc, http.MethodPut, "/settings/retention", `{"logs_days":-1,"traces_days":7,"alerts_days":14}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "3650") {
		t.Fatalf("the error does not say what the bounds are: %s", rec.Body)
	}
}

// A typo in a field name must not read as a policy of zeros, which would be a
// request to keep everything forever.
func TestRetentionUpdateRejectsAnUnknownField(t *testing.T) {
	svc := &fakeRetention{}

	rec := doRetention(t, svc, http.MethodPut, "/settings/retention",
		`{"logs_days":30,"trace_days":7}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotUpdate != nil {
		t.Fatalf("a malformed body reached the service as %+v", *svc.gotUpdate)
	}
}

// A save replaces the whole policy, so a body naming one window is a request to
// set the other to zero — which switches that stream's retention off. Nobody
// means that by omission, and it is the direction that loses data.
func TestRetentionUpdateRejectsAPartialPolicy(t *testing.T) {
	for _, body := range []string{
		`{"logs_days":30}`,
		`{"traces_days":7}`,
		`{"logs_days":30,"traces_days":null}`,
		`{"logs_days":30,"traces_days":7}`,
		`{}`,
	} {
		t.Run(body, func(t *testing.T) {
			svc := &fakeRetention{}

			rec := doRetention(t, svc, http.MethodPut, "/settings/retention", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
			if svc.gotUpdate != nil {
				t.Fatalf("a partial policy reached the service as %+v", *svc.gotUpdate)
			}
		})
	}
}

// Zero is a value, not an absence: it is how you ask for a stream to be kept
// forever, and it has to be accepted as readily as any other number.
func TestRetentionUpdateAcceptsExplicitZeros(t *testing.T) {
	svc := &fakeRetention{}

	rec := doRetention(t, svc, http.MethodPut, "/settings/retention",
		`{"logs_days":0,"traces_days":0,"alerts_days":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotUpdate == nil {
		t.Fatal("an explicit keep-everything policy did not reach the service")
	}
	if svc.gotUpdate.LogsDays != 0 || svc.gotUpdate.TracesDays != 0 || svc.gotUpdate.AlertsDays != 0 {
		t.Fatalf("update = %+v", *svc.gotUpdate)
	}
}

// Two concatenated objects would otherwise decode the first and drop the second,
// applying half of what was sent without saying so.
func TestRetentionUpdateRejectsASecondValue(t *testing.T) {
	svc := &fakeRetention{}

	rec := doRetention(t, svc, http.MethodPut, "/settings/retention",
		`{"logs_days":30,"traces_days":7,"alerts_days":14}{"logs_days":1,"traces_days":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotUpdate != nil {
		t.Fatalf("a body with two values reached the service as %+v", *svc.gotUpdate)
	}
}

func TestRetentionRunReportsWhatWent(t *testing.T) {
	logsCut := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	svc := &fakeRetention{result: retention.RunResult{
		LogsDeleted:           120,
		TracesDeleted:         44,
		TraceSummariesDeleted: 6,
		LogsCutoff:            &logsCut,
		Duration:              1500 * time.Millisecond,
	}}

	rec := doRetention(t, svc, http.MethodPost, "/retention/run", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got := decodeBody[retentionRunResponse](t, rec)
	if got.LogsDeleted != 120 || got.TracesDeleted != 44 || got.TraceSummariesDeleted != 6 {
		t.Fatalf("body = %+v", got)
	}
	if got.LogsCutoff == nil || !got.LogsCutoff.Equal(logsCut) {
		t.Fatalf("logs_cutoff = %v, want %v", got.LogsCutoff, logsCut)
	}
	// A null cutoff is how a caller tells "this stream is set to keep everything"
	// apart from "nothing on this stream had expired".
	if got.TracesCutoff != nil {
		t.Fatalf("traces_cutoff = %v, want null for a disabled axis", got.TracesCutoff)
	}
	if got.DurationMs != 1500 {
		t.Fatalf("duration_ms = %d, want 1500", got.DurationMs)
	}
}

func TestRetentionRunConflictsWithASweepInProgress(t *testing.T) {
	svc := &fakeRetention{runErr: retention.ErrRunInProgress}

	rec := doRetention(t, svc, http.MethodPost, "/retention/run", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body)
	}
}

func TestRetentionRunReportsAFailureAsAServerError(t *testing.T) {
	svc := &fakeRetention{runErr: errors.New("connection reset")}

	rec := doRetention(t, svc, http.MethodPost, "/retention/run", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body)
	}
}

// The read routes must not answer a write method, and the sweep must not be
// reachable by a GET — a link preview or a crawler would then delete data.
func TestRetentionRunIsPostOnly(t *testing.T) {
	svc := &fakeRetention{}

	rec := doRetention(t, svc, http.MethodGet, "/retention/run", "")
	if rec.Code == http.StatusOK {
		t.Fatalf("GET /retention/run answered %d", rec.Code)
	}
	if svc.runs != 0 {
		t.Fatal("a GET ran the sweep")
	}
}
