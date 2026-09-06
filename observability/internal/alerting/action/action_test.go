package action

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

func notification(kind alerting.ActionKind) alerting.Notification {
	observed, baseline := 0.41, 0.02
	return alerting.Notification{
		Kind: kind, At: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
		WatchID: "w_1", WatchName: "checkout errors", Severity: "critical",
		IncidentID: "i_1", Combinator: alerting.CombineAny, Matched: 1, Total: 2,
		Outcomes: []alerting.Outcome{
			{
				ConditionID: "c_1", Label: "error_rate gt over 15m", Truth: "true",
				Threshold: 0.05, Observed: &observed, Baseline: &baseline,
			},
			{ConditionID: "c_2", Label: "cost_usd gt over 15m", Truth: "false", Threshold: 5, Reason: "threshold_unmet"},
		},
		WindowFrom: time.Date(2026, 9, 6, 9, 45, 0, 0, time.UTC),
		WindowTo:   time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
	}
}

func spec(id, kind, params string) alerting.ActionSpec {
	return alerting.ActionSpec{ID: id, Type: kind, Params: json.RawMessage(params)}
}

func TestValidateAcceptsAWellFormedSet(t *testing.T) {
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("a_1", alerting.ActionTypeTopic, `{"deploymentId":"d1","subject":"alerts"}`),
		spec("a_2", alerting.ActionTypeEmail, `{"to":["ops@example.com"]}`),
		spec("a_3", alerting.ActionTypeLog, `{}`),
	}}
	if err := Validate(w); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRefusesWhatCannotDeliver(t *testing.T) {
	cases := []struct {
		name string
		spec alerting.ActionSpec
		want error
	}{
		{"no id", spec("", alerting.ActionTypeLog, `{}`), alerting.ErrInvalidParams},
		{"unknown type", spec("a", "carrier-pigeon", `{}`), alerting.ErrUnknownAction},
		{"topic without a deployment", spec("a", alerting.ActionTypeTopic, `{"subject":"alerts"}`), alerting.ErrInvalidParams},
		{"topic without a subject", spec("a", alerting.ActionTypeTopic, `{"deploymentId":"d1"}`), alerting.ErrInvalidParams},
		// A publish subject may not contain a wildcard: it would go nowhere while
		// looking exactly like a watch that was working.
		{"wildcard subject", spec("a", alerting.ActionTypeTopic, `{"deploymentId":"d1","subject":"alerts.*"}`), alerting.ErrInvalidParams},
		{"greedy subject", spec("a", alerting.ActionTypeTopic, `{"deploymentId":"d1","subject":"alerts.>"}`), alerting.ErrInvalidParams},
		{"system subject", spec("a", alerting.ActionTypeTopic, `{"deploymentId":"d1","subject":"system:internal.alerts"}`), alerting.ErrInvalidParams},
		{"email without a recipient", spec("a", alerting.ActionTypeEmail, `{"to":[]}`), alerting.ErrInvalidParams},
		{"email to something that is not an address", spec("a", alerting.ActionTypeEmail, `{"to":["not an address"]}`), alerting.ErrInvalidParams},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(alerting.Watch{Actions: []alerting.ActionSpec{c.spec}})
			if !errors.Is(err, c.want) {
				t.Errorf("error = %v, want %v", err, c.want)
			}
		})
	}
}

func TestValidateRefusesDuplicateActionIDs(t *testing.T) {
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("same", alerting.ActionTypeLog, `{}`),
		spec("same", alerting.ActionTypeLog, `{}`),
	}}
	if err := Validate(w); !errors.Is(err, alerting.ErrInvalidWatch) {
		t.Errorf("error = %v, want ErrInvalidWatch", err)
	}
}

// Validation answers "is this action well-formed", not "can this particular
// process perform it right now". The second is a deployment fact, and letting it
// decide would make a watch unsaveable on a machine that happens to have no
// broker configured.
func TestValidateDoesNotDependOnThisProcessesDependencies(t *testing.T) {
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("a_1", alerting.ActionTypeTopic, `{"deploymentId":"d1","subject":"alerts"}`),
		spec("a_2", alerting.ActionTypeEmail, `{"to":["ops@example.com"]}`),
	}}
	if err := Validate(w); err != nil {
		t.Fatalf("a well-formed watch was refused by a process with no broker: %v", err)
	}
}

// A process that genuinely cannot perform an action records that, rather than
// failing silently or refusing to run at all.
func TestNotifyRecordsWhatThisProcessCannotDo(t *testing.T) {
	d := NewDispatcher(nil, nil)
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("a_1", alerting.ActionTypeTopic, `{"deploymentId":"d1","subject":"alerts"}`),
		spec("a_2", alerting.ActionTypeEmail, `{"to":["ops@example.com"]}`),
	}}
	results := d.Notify(t.Context(), w, notification(alerting.ActionOpen))

	if len(results) != 2 {
		t.Fatalf("%d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Delivered() {
			t.Errorf("action %s reported success with nothing to deliver through", r.ActionID)
		}
	}
}

// One action failing must not stop the next: a watch that emails and publishes
// has two audiences, and the second has done nothing wrong.
func TestOneFailingActionDoesNotStopTheOthers(t *testing.T) {
	d := NewDispatcher(nil, nil)
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("a_1", alerting.ActionTypeEmail, `{"to":["ops@example.com"]}`),
		spec("a_2", alerting.ActionTypeLog, `{}`),
	}}
	results := d.Notify(t.Context(), w, notification(alerting.ActionOpen))

	if len(results) != 2 {
		t.Fatalf("%d results, want 2", len(results))
	}
	if results[0].Delivered() {
		t.Error("the mail action reported success with no mailer")
	}
	if !results[1].Delivered() {
		t.Errorf("the log action was skipped after an earlier failure: %s", results[1].Err)
	}
}

func TestEmailPostsToTheOrchestrator(t *testing.T) {
	var got sendRequest
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messageId":"m_1"}`))
	}))
	defer server.Close()

	d := NewDispatcher(nil, NewMailer(server.URL))
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("a_1", alerting.ActionTypeEmail, `{"to":["ops@example.com"]}`),
	}}
	results := d.Notify(t.Context(), w, notification(alerting.ActionOpen))

	if !results[0].Delivered() {
		t.Fatalf("send failed: %s", results[0].Err)
	}
	if path != sendPath {
		t.Errorf("posted to %q, want %q", path, sendPath)
	}
	if len(got.To) != 1 || got.To[0] != "ops@example.com" {
		t.Errorf("recipients = %v", got.To)
	}
	if !strings.Contains(got.Subject, "checkout errors is firing") {
		t.Errorf("subject = %q", got.Subject)
	}
	if !strings.Contains(got.Subject, "critical") {
		t.Errorf("subject %q does not carry the severity", got.Subject)
	}
	// The body renders the same account of what happened that the incident page
	// does, so the two readers are reading the same thing.
	for _, want := range []string{"1 of 2 conditions matched", "error_rate gt over 15m", "0.41", "0.05", "0.02"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("body is missing %q:\n%s", want, got.Text)
		}
	}
}

// The orchestrator's own reason has to come through: "email is not configured"
// and "the provider was unreachable" want different things done about them.
func TestEmailCarriesTheOrchestratorsReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"email is not configured"}`))
	}))
	defer server.Close()

	d := NewDispatcher(nil, NewMailer(server.URL))
	w := alerting.Watch{Actions: []alerting.ActionSpec{
		spec("a_1", alerting.ActionTypeEmail, `{"to":["ops@example.com"]}`),
	}}
	results := d.Notify(t.Context(), w, notification(alerting.ActionOpen))

	if results[0].Delivered() {
		t.Fatal("a 409 reported success")
	}
	if !strings.Contains(results[0].Err, "email is not configured") {
		t.Errorf("error = %q, want the orchestrator's reason", results[0].Err)
	}
}

func TestResolvedNotificationsReadAsEndings(t *testing.T) {
	for _, c := range []struct {
		kind alerting.ActionKind
		want string
	}{
		{alerting.ActionOpen, "checkout errors is firing"},
		{alerting.ActionRenotify, "checkout errors is still firing"},
		{alerting.ActionResolve, "checkout errors recovered"},
	} {
		n := notification(c.kind)
		if got := n.Headline(); got != c.want {
			t.Errorf("%s headline = %q, want %q", c.kind, got, c.want)
		}
	}
	closed := notification(alerting.ActionClose)
	closed.Reason = alerting.ClosedStale
	if got := closed.Headline(); !strings.Contains(got, alerting.ClosedStale) {
		t.Errorf("a closed episode's headline %q does not say why", got)
	}
	// A recovery must not carry a severity prefix: it is not an escalation.
	if strings.Contains(subjectFor(notification(alerting.ActionResolve)), "critical") {
		t.Error("a recovery was announced at severity")
	}
}
