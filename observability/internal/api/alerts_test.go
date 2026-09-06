package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// fakeAlerts records what it was asked to do and returns canned answers.
type fakeAlerts struct {
	watch      alerting.Watch
	watches    []alerting.Due
	incidents  []alerting.Incident
	history    []alerting.EvaluationRecord
	evaluation alerting.Evaluation
	gotWatch   *alerting.Watch
	gotFilter  *alerting.HistoryFilter
	gotIncFilt *alerting.IncidentFilter
	gotMute    *time.Time
	gotUser    string
	deleted    string
	acked      string
	err        error
	previewErr error
}

func (f *fakeAlerts) Create(_ context.Context, w alerting.Watch, user string) (alerting.Watch, error) {
	f.gotWatch, f.gotUser = &w, user
	if f.err != nil {
		return alerting.Watch{}, f.err
	}
	w.ID = "w_1"
	return w, nil
}

func (f *fakeAlerts) Update(_ context.Context, w alerting.Watch, user string) (alerting.Watch, error) {
	f.gotWatch, f.gotUser = &w, user
	return w, f.err
}

func (f *fakeAlerts) Get(context.Context, string) (alerting.Watch, error) {
	return f.watch, f.err
}

func (f *fakeAlerts) Delete(_ context.Context, id string) error {
	f.deleted = id
	return f.err
}

func (f *fakeAlerts) List(context.Context) ([]alerting.Due, error) { return f.watches, f.err }

func (f *fakeAlerts) Preview(_ context.Context, w alerting.Watch) (alerting.Evaluation, error) {
	f.gotWatch = &w
	return f.evaluation, f.previewErr
}

func (f *fakeAlerts) Mute(_ context.Context, _ string, until time.Time) error {
	f.gotMute = &until
	return f.err
}

func (f *fakeAlerts) Acknowledge(_ context.Context, id, user string) error {
	f.acked, f.gotUser = id, user
	return f.err
}

func (f *fakeAlerts) Incidents(_ context.Context, filter alerting.IncidentFilter) ([]alerting.Incident, error) {
	f.gotIncFilt = &filter
	return f.incidents, f.err
}

func (f *fakeAlerts) History(_ context.Context, filter alerting.HistoryFilter) ([]alerting.EvaluationRecord, error) {
	f.gotFilter = &filter
	return f.history, f.err
}

func doAlerts(t *testing.T, svc AlertService, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewAlertsHandler(svc).Register(mux)

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Octo-User-Id", "u_1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

const watchJSON = `{
  "name":"checkout errors","enabled":true,"severity":"warning","combinator":"any",
  "on_no_data":"ok","step_seconds":60,"interval_seconds":60,"for_seconds":300,
  "conditions":[{"id":"c_1","type":"threshold","source":"traces","metric":"error_rate",
                 "params":{"op":"gt","threshold":0.05}}],
  "actions":[{"id":"a_1","type":"email","params":{"to":["ops@example.com"]}}]
}`

// The definition has to survive the wire in the shape the domain decodes, or a
// watch saves cleanly and evaluates something else.
func TestCreateRoundTripsTheDefinition(t *testing.T) {
	svc := &fakeAlerts{}

	rec := doAlerts(t, svc, http.MethodPost, "/alerts/watches", watchJSON)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotWatch == nil {
		t.Fatal("nothing reached the service")
	}
	got := *svc.gotWatch
	if got.Name != "checkout errors" || got.Combinator != alerting.CombineAny {
		t.Errorf("watch = %+v", got)
	}
	if got.Step != time.Minute || got.For != 5*time.Minute {
		t.Errorf("durations came through as step=%s for=%s", got.Step, got.For)
	}
	if len(got.Conditions) != 1 || got.Conditions[0].Metric != "error_rate" {
		t.Fatalf("conditions = %+v", got.Conditions)
	}
	// The parameters must survive as the domain's own decoder sees them.
	if _, err := alerting.NewCondition(got.Conditions[0], time.Minute); err != nil {
		t.Errorf("the decoded condition does not build: %v", err)
	}
	if len(got.Actions) != 1 || got.Actions[0].Type != alerting.ActionTypeEmail {
		t.Errorf("actions = %+v", got.Actions)
	}
	if svc.gotUser != "u_1" {
		t.Errorf("acting user %q, want the one the BFF named", svc.gotUser)
	}

	body := decodeBody[watchBody](t, rec)
	if body.ID != "w_1" || len(body.Conditions) != 1 {
		t.Errorf("response = %+v", body)
	}
}

// A validation failure has to carry the domain's own message: it names the field
// and the bound, and "invalid watch" throws that away.
func TestValidationFailuresCarryTheirReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid", alerting.ErrInvalidWatch, http.StatusBadRequest},
		{"bad params", alerting.ErrInvalidParams, http.StatusBadRequest},
		{"unknown condition", alerting.ErrUnknownCondition, http.StatusBadRequest},
		{"unknown action", alerting.ErrUnknownAction, http.StatusBadRequest},
		{"nested", alerting.ErrNestedConditions, http.StatusBadRequest},
		{"name taken", alerting.ErrNameTaken, http.StatusConflict},
		{"too many", alerting.ErrTooManyWatches, http.StatusConflict},
		{"not found", alerting.ErrWatchNotFound, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doAlerts(t, &fakeAlerts{err: c.err}, http.MethodPost, "/alerts/watches", watchJSON)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.want, rec.Body)
			}
			if c.want == http.StatusBadRequest && !strings.Contains(rec.Body.String(), c.err.Error()) {
				t.Errorf("body %s does not carry the reason %q", rec.Body, c.err)
			}
		})
	}
}

// The address is what the caller meant, whatever the body says.
func TestUpdateTakesItsIDFromThePath(t *testing.T) {
	svc := &fakeAlerts{watch: alerting.Watch{ID: "w_9"}}
	body := strings.Replace(watchJSON, `"name"`, `"id":"w_someone_elses","name"`, 1)

	rec := doAlerts(t, svc, http.MethodPut, "/alerts/watches/w_9", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotWatch.ID != "w_9" {
		t.Errorf("updated %q, want the id in the path", svc.gotWatch.ID)
	}
}

func TestDeleteAndAcknowledge(t *testing.T) {
	svc := &fakeAlerts{}
	if rec := doAlerts(t, svc, http.MethodDelete, "/alerts/watches/w_1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (body %s)", rec.Code, rec.Body)
	}
	if svc.deleted != "w_1" {
		t.Errorf("deleted %q", svc.deleted)
	}

	if rec := doAlerts(t, svc, http.MethodPost, "/alerts/incidents/i_1/ack", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("ack status = %d, want 204 (body %s)", rec.Code, rec.Body)
	}
	if svc.acked != "i_1" || svc.gotUser != "u_1" {
		t.Errorf("acknowledged %q by %q", svc.acked, svc.gotUser)
	}

	notFound := &fakeAlerts{err: alerting.ErrIncidentNotFound}
	if rec := doAlerts(t, notFound, http.MethodPost, "/alerts/incidents/i_1/ack", ""); rec.Code != http.StatusNotFound {
		t.Errorf("re-ack status = %d, want 404", rec.Code)
	}
}

// A mute with no end is a watch switched off without the list saying so, which
// is what `enabled` is for.
func TestMute(t *testing.T) {
	svc := &fakeAlerts{}
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	rec := doAlerts(t, svc, http.MethodPost, "/alerts/watches/w_1/mute", `{"until":"`+until+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body)
	}
	if svc.gotMute == nil || svc.gotMute.IsZero() {
		t.Error("the mute did not reach the service")
	}

	// Null lifts it, which is how somebody un-mutes without picking a time in
	// the past.
	svc = &fakeAlerts{}
	if rec := doAlerts(t, svc, http.MethodPost, "/alerts/watches/w_1/mute", `{"until":null}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if svc.gotMute == nil || !svc.gotMute.IsZero() {
		t.Errorf("mute = %v, want the zero time", svc.gotMute)
	}

	far := time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)
	rec = doAlerts(t, &fakeAlerts{}, http.MethodPost, "/alerts/watches/w_1/mute", `{"until":"`+far+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a mute a year out", rec.Code)
	}
}

func TestPreviewReturnsEveryOutcome(t *testing.T) {
	observed := 0.41
	svc := &fakeAlerts{evaluation: alerting.Evaluation{
		Status: alerting.StatusFiring, Verdict: alerting.True, Matched: 1, Total: 2, Degraded: true,
		Outcomes: []alerting.Outcome{{ConditionID: "c_1", Truth: "true", Observed: &observed, Threshold: 0.05}},
	}}

	rec := doAlerts(t, svc, http.MethodPost, "/alerts/preview", watchJSON)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got := decodeBody[previewResponse](t, rec)
	if got.Status != string(alerting.StatusFiring) || got.Verdict != "true" {
		t.Errorf("response = %+v", got)
	}
	if got.Matched != 1 || got.Total != 2 || !got.Degraded {
		t.Errorf("response = %+v", got)
	}
	if len(got.Outcomes) != 1 || got.Outcomes[0].Observed == nil {
		t.Errorf("outcomes = %+v", got.Outcomes)
	}
}

// An empty result renders as [] rather than null, so a client can iterate
// without a nil check on every row.
func TestEmptyListsRenderAsArrays(t *testing.T) {
	rec := doAlerts(t, &fakeAlerts{}, http.MethodGet, "/alerts/incidents", "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("body = %s", rec.Body)
	}
	rec = doAlerts(t, &fakeAlerts{}, http.MethodGet, "/alerts/watches", "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestHistoryFiltersReachTheService(t *testing.T) {
	svc := &fakeAlerts{}
	rec := doAlerts(t, svc, http.MethodGet,
		"/alerts/watches/w_1/evaluations?notable=true&status=firing&status=error"+
			"&incidentId=i_1&from=2026-09-06T10:00:00Z&to=2026-09-06T11:00:00Z&limit=5", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	got := *svc.gotFilter
	if got.WatchID != "w_1" || got.IncidentID != "i_1" || !got.NotableOnly || got.Limit != 5 {
		t.Errorf("filter = %+v", got)
	}
	if len(got.Statuses) != 2 {
		t.Errorf("statuses = %v, want both", got.Statuses)
	}
	if got.From == nil || got.To == nil {
		t.Errorf("range = %v..%v", got.From, got.To)
	}
}

func TestHistoryRejectsAMalformedRangeOrCursor(t *testing.T) {
	for _, target := range []string{
		"/alerts/evaluations?from=yesterday",
		"/alerts/evaluations?to=soon",
		"/alerts/evaluations?before=nonsense",
	} {
		if rec := doAlerts(t, &fakeAlerts{}, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

// The cursor is offered only on a full page: one on a short page invites a
// request that can only come back empty.
func TestHistoryOffersACursorOnlyWhenThereMayBeMore(t *testing.T) {
	rows := make([]alerting.EvaluationRecord, 3)
	for i := range rows {
		rows[i] = alerting.EvaluationRecord{
			ID: "e_" + string(rune('a'+i)), EvaluatedAt: time.Now().Add(-time.Duration(i) * time.Minute),
		}
	}
	full := doAlerts(t, &fakeAlerts{history: rows}, http.MethodGet, "/alerts/evaluations?limit=3", "")
	if got := decodeBody[evaluationListResponse](t, full); got.NextBefore == "" {
		t.Error("a full page offered no cursor")
	}
	short := doAlerts(t, &fakeAlerts{history: rows}, http.MethodGet, "/alerts/evaluations?limit=10", "")
	if got := decodeBody[evaluationListResponse](t, short); got.NextBefore != "" {
		t.Errorf("a short page offered the cursor %q", got.NextBefore)
	}
}

func TestIncidentFiltersReachTheService(t *testing.T) {
	svc := &fakeAlerts{}
	doAlerts(t, svc, http.MethodGet, "/alerts/incidents?watchId=w_1&open=true&limit=7", "")
	got := *svc.gotIncFilt
	if got.WatchID != "w_1" || !got.OpenOnly || got.Limit != 7 {
		t.Errorf("filter = %+v", got)
	}
}

func TestBadBodiesAreRefusedBeforeTheService(t *testing.T) {
	for _, body := range []string{`{`, `{"conditions":[[]]}`, `{"name":"x"}{"name":"y"}`} {
		svc := &fakeAlerts{}
		rec := doAlerts(t, svc, http.MethodPost, "/alerts/watches", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
		if svc.gotWatch != nil {
			t.Errorf("body %s reached the service", body)
		}
	}
}

func TestListRendersStateBesideTheDefinition(t *testing.T) {
	when := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	svc := &fakeAlerts{watches: []alerting.Due{{
		Watch: alerting.Watch{ID: "w_1", Name: "checkout", Step: time.Minute},
		State: alerting.State{Phase: alerting.PhaseFiring, Since: when, IncidentID: "i_1"},
	}}}

	rec := doAlerts(t, svc, http.MethodGet, "/alerts/watches", "")
	got := decodeBody[watchListResponse](t, rec)
	if len(got.Items) != 1 {
		t.Fatalf("%d items, want 1", len(got.Items))
	}
	if got.Items[0].State.Phase != string(alerting.PhaseFiring) || got.Items[0].State.IncidentID != "i_1" {
		t.Errorf("state = %+v", got.Items[0].State)
	}
	// A zero timestamp renders as null rather than as year one, which a client
	// would otherwise have to recognise.
	if got.Items[0].State.LastEvalAt != nil {
		t.Errorf("last_eval_at = %v, want null for a watch that has not run", got.Items[0].State.LastEvalAt)
	}
}

func TestListAndHistoryReportAFailure(t *testing.T) {
	boom := &fakeAlerts{err: errors.New("postgres is unreachable")}
	for _, target := range []string{"/alerts/watches", "/alerts/incidents", "/alerts/evaluations"} {
		if rec := doAlerts(t, boom, http.MethodGet, target, ""); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", target, rec.Code)
		}
	}
}
