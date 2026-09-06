package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

var storeNow = time.Date(2033, 6, 1, 9, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run alerting store tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		// Deleting the watches is enough: state, incidents and evaluations all
		// cascade, which is itself asserted below.
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert_watches`)
		pool.Close()
	})
	_, _ = pool.Exec(t.Context(), `DELETE FROM alert_watches`)
	return New(pool)
}

func sampleWatch(t *testing.T, name string) alerting.Watch {
	t.Helper()
	w := alerting.Watch{
		Name: name, Description: "watches the checkout error rate", Enabled: true, Severity: "warning",
		Combinator: alerting.CombineAny, OnNoData: alerting.NoDataOK,
		Step: time.Minute, Interval: time.Minute, For: 3 * time.Minute, Renotify: 10 * time.Minute,
		Conditions: []alerting.ConditionSpec{{
			ID: "c_1", Type: alerting.KindThreshold, Source: alerting.SourceTraces, Metric: "error_rate",
			Scope:  alerting.Scope{AppName: "checkout"},
			Params: json.RawMessage(`{"op":"gt","threshold":0.05}`),
		}},
		Actions: []alerting.ActionSpec{{ID: "a_1", Type: "email", Params: json.RawMessage(`{"to":["ops@x"]}`)}},
	}
	hash, err := w.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	w.DefinitionHash = hash
	return w
}

func mustCreate(t *testing.T, s *Store, name string) alerting.Watch {
	t.Helper()
	w, err := s.Create(t.Context(), sampleWatch(t, name), "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return w
}

func TestCreateRoundTripsTheDefinition(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")

	got, err := s.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "checkout errors" || got.Combinator != alerting.CombineAny {
		t.Errorf("watch came back as %+v", got)
	}
	if got.Step != time.Minute || got.For != 3*time.Minute || got.Renotify != 10*time.Minute {
		t.Errorf("durations came back as step=%s for=%s renotify=%s", got.Step, got.For, got.Renotify)
	}
	if len(got.Conditions) != 1 || got.Conditions[0].ID != "c_1" ||
		got.Conditions[0].Scope.AppName != "checkout" {
		t.Errorf("conditions came back as %+v", got.Conditions)
	}
	if len(got.Actions) != 1 || got.Actions[0].Type != "email" {
		t.Errorf("actions came back as %+v", got.Actions)
	}
	// It must still build, which is the real assertion: a definition that
	// survived the round trip in shape but not in meaning is worse than one that
	// failed to decode.
	if _, err := alerting.Build(got); err != nil {
		t.Errorf("the stored definition no longer builds: %v", err)
	}
}

// A watch is never visible without somewhere for its first evaluation to land,
// and it is due immediately rather than after one interval of silence.
func TestCreateAlsoCreatesStateDueNow(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")

	due, err := s.Due(t.Context(), storeNow, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].Watch.ID != created.ID {
		t.Fatalf("due list = %+v, want the watch just created", due)
	}
	if due[0].State.Phase != alerting.PhaseOK || due[0].State.DefinitionHash != created.DefinitionHash {
		t.Errorf("state = %+v, want a machine at rest carrying the definition hash", due[0].State)
	}
}

func TestNamesAreUniqueCaseInsensitively(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, "checkout errors")
	_, err := s.Create(t.Context(), sampleWatch(t, "CHECKOUT ERRORS"), "")
	if !errors.Is(err, alerting.ErrNameTaken) {
		t.Errorf("error = %v, want ErrNameTaken", err)
	}
}

func TestUpdateReplacesTheDefinition(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")

	next := created
	next.Name = "checkout errors, retuned"
	next.Conditions[0].Params = json.RawMessage(`{"op":"gt","threshold":0.5}`)
	updated, err := s.Update(t.Context(), next, "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DefinitionHash == created.DefinitionHash {
		t.Error("retuning a threshold did not move the definition hash")
	}
	// The state row is deliberately untouched here: Step resets it on the next
	// evaluation when it sees the hashes disagree, which keeps every reason a
	// hold can restart in the one function that owns holds.
	due, _ := s.Due(t.Context(), storeNow, 10)
	if due[0].State.DefinitionHash != created.DefinitionHash {
		t.Error("the store reset the state itself, splitting hold ownership")
	}
}

func TestGetUpdateAndDeleteReportAMissingWatch(t *testing.T) {
	s := newStore(t)
	missing := "00000000-0000-0000-0000-0000000000ff"

	if _, err := s.Get(t.Context(), missing); !errors.Is(err, alerting.ErrWatchNotFound) {
		t.Errorf("get error = %v, want ErrWatchNotFound", err)
	}
	if err := s.Delete(t.Context(), missing); !errors.Is(err, alerting.ErrWatchNotFound) {
		t.Errorf("delete error = %v, want ErrWatchNotFound", err)
	}
	w := sampleWatch(t, "nothing")
	w.ID = missing
	if _, err := s.Update(t.Context(), w, ""); !errors.Is(err, alerting.ErrWatchNotFound) {
		t.Errorf("update error = %v, want ErrWatchNotFound", err)
	}
}

func TestDisabledWatchesAreNeverDue(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.Enabled = false
	if _, err := s.Update(t.Context(), created, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	due, err := s.Due(t.Context(), storeNow, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("a disabled watch was listed as due: %+v", due)
	}
	// But it still lists, because somebody has to be able to turn it back on.
	listed, _ := s.List(t.Context())
	if len(listed) != 1 {
		t.Errorf("the list has %d watches, want the disabled one", len(listed))
	}
}

// firing builds the Result one firing evaluation would produce.
func firing(w alerting.Watch, prev alerting.State, at time.Time, verdict alerting.Truth) alerting.Result {
	observed := 0.42
	ev := alerting.Evaluation{
		At: at, Combinator: w.Combinator, Verdict: verdict, Matched: 1, Total: 1,
		Outcomes: []alerting.Outcome{{
			ConditionID: "c_1", Kind: alerting.KindThreshold, Label: "error_rate gt over 5m",
			Threshold: 0.05, Observed: &observed, Truth: verdict.String(),
		}},
		WindowFrom: at.Add(-5 * time.Minute), WindowTo: at,
	}
	switch verdict {
	case alerting.True:
		ev.Status = alerting.StatusFiring
	case alerting.Unknown:
		ev.Status = alerting.StatusInsufficient
	default:
		ev.Status = alerting.StatusOK
	}
	next, actions := alerting.Step(prev, w, ev, at)
	return alerting.Result{
		Watch: w, Evaluation: ev, Previous: prev, State: next,
		Actions: actions, Duration: 12 * time.Millisecond,
	}
}

func TestRecordWritesHistoryStateAndIncidentTogether(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}

	// Three firing evaluations: two pending, the third opens the episode.
	for i := range 3 {
		at := storeNow.Add(time.Duration(i) * time.Minute)
		r := firing(created, prev, at, alerting.True)
		if _, err := s.Record(t.Context(), r); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		due, _ := s.Due(t.Context(), at.Add(time.Hour), 10)
		prev = due[0].State
	}

	if prev.Phase != alerting.PhaseFiring {
		t.Fatalf("phase %s, want firing after three consecutive evaluations", prev.Phase)
	}
	if prev.IncidentID == "" {
		t.Fatal("no incident id was written back to the state")
	}

	history, err := s.History(t.Context(), alerting.HistoryFilter{WatchID: created.ID})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("%d history rows, want 3 — every tick is recorded, not only the transitions", len(history))
	}
	// Newest first, and the newest is the one that transitioned.
	if !history[0].Transitioned || history[0].Phase != alerting.PhaseFiring {
		t.Errorf("newest row = %+v, want the transition into firing", history[0])
	}
	if history[0].IncidentID != prev.IncidentID {
		t.Error("the transition row does not carry the incident it opened")
	}
	// And it explains itself without anything having to be recomputed.
	if len(history[0].Outcomes) != 1 || history[0].Outcomes[0].Threshold != 0.05 ||
		history[0].Outcomes[0].Observed == nil {
		t.Errorf("outcomes came back as %+v", history[0].Outcomes)
	}

	incidents, err := s.Incidents(t.Context(), alerting.IncidentFilter{OpenOnly: true})
	if err != nil {
		t.Fatalf("incidents: %v", err)
	}
	if len(incidents) != 1 || !incidents[0].Open() || incidents[0].WatchName != created.Name {
		t.Fatalf("incidents = %+v, want one open episode naming its watch", incidents)
	}
	if incidents[0].Evaluations != 1 {
		t.Errorf("the episode counted %d evaluations, want 1", incidents[0].Evaluations)
	}
}

func TestRecordClosesAnEpisodeOnRecovery(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.For = 0
	created, _ = s.Update(t.Context(), created, "")

	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}
	step := func(at time.Time, verdict alerting.Truth) {
		t.Helper()
		if _, err := s.Record(t.Context(), firing(created, prev, at, verdict)); err != nil {
			t.Fatalf("record: %v", err)
		}
		due, _ := s.Due(t.Context(), at.Add(time.Hour), 10)
		prev = due[0].State
	}

	step(storeNow, alerting.True)
	opened := prev.IncidentID
	step(storeNow.Add(time.Minute), alerting.False)
	step(storeNow.Add(2*time.Minute), alerting.False)

	if prev.Phase != alerting.PhaseOK {
		t.Fatalf("phase %s, want ok after two clean evaluations", prev.Phase)
	}
	incidents, _ := s.Incidents(t.Context(), alerting.IncidentFilter{WatchID: created.ID})
	if len(incidents) != 1 {
		t.Fatalf("%d incidents, want 1", len(incidents))
	}
	if incidents[0].ID != opened || incidents[0].Open() {
		t.Errorf("incident %+v, want the opened one closed", incidents[0])
	}
	if incidents[0].ClosedReason != alerting.ClosedResolved {
		t.Errorf("closed reason %q, want %q", incidents[0].ClosedReason, alerting.ClosedResolved)
	}
}

// The guard that stops an evaluator whose lease has already moved from writing
// an older decision over a newer one. Nothing is written at all: the whole
// transaction rolls back.
func TestRecordRefusesAStaleEvaluation(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}

	if _, err := s.Record(t.Context(), firing(created, prev, storeNow.Add(time.Minute), alerting.True)); err != nil {
		t.Fatalf("record the newer evaluation: %v", err)
	}
	_, err := s.Record(t.Context(), firing(created, prev, storeNow, alerting.True))
	if !errors.Is(err, alerting.ErrStaleEvaluation) {
		t.Fatalf("error = %v, want ErrStaleEvaluation", err)
	}
	history, _ := s.History(t.Context(), alerting.HistoryFilter{WatchID: created.ID})
	if len(history) != 1 {
		t.Errorf("%d history rows, want 1 — the refused write must roll back entirely", len(history))
	}
}

func TestHistoryFiltersAndPages(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}

	verdicts := []alerting.Truth{alerting.False, alerting.False, alerting.True, alerting.Unknown, alerting.False}
	for i, v := range verdicts {
		at := storeNow.Add(time.Duration(i) * time.Minute)
		if _, err := s.Record(t.Context(), firing(created, prev, at, v)); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		due, _ := s.Due(t.Context(), at.Add(time.Hour), 10)
		prev = due[0].State
	}

	all, _ := s.History(t.Context(), alerting.HistoryFilter{WatchID: created.ID})
	if len(all) != 5 {
		t.Fatalf("%d rows, want 5", len(all))
	}

	// The default view of a table where most rows say nothing happened.
	notable, _ := s.History(t.Context(), alerting.HistoryFilter{WatchID: created.ID, NotableOnly: true})
	if len(notable) != 2 {
		t.Errorf("%d notable rows, want 2 (the firing one and the undecided one)", len(notable))
	}

	byStatus, _ := s.History(t.Context(), alerting.HistoryFilter{
		WatchID: created.ID, Statuses: []alerting.Status{alerting.StatusInsufficient}})
	if len(byStatus) != 1 {
		t.Errorf("%d insufficient rows, want 1", len(byStatus))
	}

	// Keyset paging, on the composite cursor.
	first, _ := s.History(t.Context(), alerting.HistoryFilter{WatchID: created.ID, Limit: 2})
	if len(first) != 2 {
		t.Fatalf("%d rows on the first page, want 2", len(first))
	}
	second, _ := s.History(t.Context(), alerting.HistoryFilter{
		WatchID: created.ID, Limit: 2,
		Before: &alerting.HistoryCursor{At: first[1].EvaluatedAt, ID: first[1].ID},
	})
	if len(second) != 2 {
		t.Fatalf("%d rows on the second page, want 2", len(second))
	}
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Errorf("row %s appears on both pages", a.ID)
			}
		}
	}
}

// A tick evaluates every due watch inside the same millisecond, so the cursor has
// to break ties on the id or a page boundary drops or repeats rows.
func TestHistoryPagesThroughTiedTimestamps(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	for range 6 {
		_, err := s.pool.Exec(t.Context(), `
			INSERT INTO alert_evaluations (watch_id, evaluated_at, status, matched, total, detail)
			VALUES ($1::uuid, $2, 'ok', 0, 1, '[]'::jsonb)`, created.ID, storeNow)
		if err != nil {
			t.Fatalf("seed a tied row: %v", err)
		}
	}

	seen := map[string]bool{}
	var cursor *alerting.HistoryCursor
	for range 4 {
		page, err := s.History(t.Context(), alerting.HistoryFilter{
			WatchID: created.ID, Limit: 2, Before: cursor})
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if seen[row.ID] {
				t.Errorf("row %s was returned twice across pages that all share a timestamp", row.ID)
			}
			seen[row.ID] = true
		}
		last := page[len(page)-1]
		cursor = &alerting.HistoryCursor{At: last.EvaluatedAt, ID: last.ID}
	}
	if len(seen) != 6 {
		t.Errorf("paging saw %d of 6 tied rows", len(seen))
	}
}

func TestDeleteCascades(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.For = 0
	created, _ = s.Update(t.Context(), created, "")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}
	if _, err := s.Record(t.Context(), firing(created, prev, storeNow, alerting.True)); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := s.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, table := range []string{"alert_watch_state", "alert_incidents", "alert_evaluations"} {
		var n int
		if err := s.pool.QueryRow(t.Context(),
			`SELECT count(*) FROM `+table+` WHERE watch_id = $1::uuid`, created.ID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows after the watch was deleted", table, n)
		}
	}
}

func TestRetireClosesAnOpenEpisode(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.For = 0
	created, _ = s.Update(t.Context(), created, "")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}
	if _, err := s.Record(t.Context(), firing(created, prev, storeNow, alerting.True)); err != nil {
		t.Fatalf("record: %v", err)
	}

	if err := s.Retire(t.Context(), created.ID, alerting.ClosedDisabled, storeNow.Add(time.Minute)); err != nil {
		t.Fatalf("retire: %v", err)
	}
	incidents, _ := s.Incidents(t.Context(), alerting.IncidentFilter{WatchID: created.ID})
	if len(incidents) != 1 || incidents[0].Open() ||
		incidents[0].ClosedReason != alerting.ClosedDisabled {
		t.Errorf("incident %+v, want it closed as disabled", incidents[0])
	}
	due, _ := s.Due(t.Context(), storeNow.Add(time.Hour), 10)
	if due[0].State.Phase != alerting.PhaseOK || due[0].State.IncidentID != "" {
		t.Errorf("state %+v, want a machine at rest with no episode", due[0].State)
	}
}

func TestAcknowledgeAndMute(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.For = 0
	created, _ = s.Update(t.Context(), created, "")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}
	if _, err := s.Record(t.Context(), firing(created, prev, storeNow, alerting.True)); err != nil {
		t.Fatalf("record: %v", err)
	}
	incidents, _ := s.Incidents(t.Context(), alerting.IncidentFilter{WatchID: created.ID})

	if err := s.Acknowledge(t.Context(), incidents[0].ID, "", storeNow); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	// Twice is not an error the second time round either, but it must not
	// silently re-stamp somebody else's acknowledgement.
	if err := s.Acknowledge(t.Context(), incidents[0].ID, "", storeNow.Add(time.Hour)); !errors.Is(err, alerting.ErrIncidentNotFound) {
		t.Errorf("second acknowledge = %v, want ErrIncidentNotFound", err)
	}

	until := storeNow.Add(time.Hour)
	if err := s.Mute(t.Context(), created.ID, until); err != nil {
		t.Fatalf("mute: %v", err)
	}
	due, _ := s.Due(t.Context(), storeNow.Add(2*time.Hour), 10)
	if !due[0].State.Muted(storeNow.Add(time.Minute)) {
		t.Error("the watch does not report itself muted")
	}
	// Muting suppresses notifications, not evaluation: it must still be due.
	if len(due) != 1 {
		t.Error("a muted watch stopped being evaluated")
	}
}

func TestMarkInvalidParksAWatchAndSaysWhy(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	until := storeNow.Add(time.Hour)

	if err := s.MarkInvalid(t.Context(), created.ID, until, `unknown condition type "sorcery"`); err != nil {
		t.Fatalf("mark invalid: %v", err)
	}
	if due, _ := s.Due(t.Context(), storeNow, 10); len(due) != 0 {
		t.Error("an invalid watch is still consuming a slot on every tick")
	}
	listed, _ := s.List(t.Context())
	if listed[0].State.Phase != alerting.PhaseInvalid {
		t.Errorf("phase %s, want invalid — the list must say it is not being evaluated", listed[0].State.Phase)
	}
	history, err := s.History(t.Context(), alerting.HistoryFilter{WatchID: created.ID})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || history[0].Reason != alerting.ReasonWatchInvalid || history[0].Err == "" {
		t.Errorf("history = %+v, want one row saying why it could not be evaluated", history)
	}
}

func TestCountAndDefer(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	mustCreate(t, s, "cost")

	n, err := s.Count(t.Context())
	if err != nil || n != 2 {
		t.Fatalf("count = (%d, %v), want (2, nil)", n, err)
	}
	if err := s.Defer(t.Context(), created.ID, storeNow.Add(time.Hour)); err != nil {
		t.Fatalf("defer: %v", err)
	}
	due, _ := s.Due(t.Context(), storeNow, 10)
	if len(due) != 1 || due[0].Watch.ID == created.ID {
		t.Errorf("due = %+v, want only the watch that was not deferred", due)
	}
}

// The incident id is minted during the write, so Record has to hand back the
// state it actually wrote — otherwise the notification that announces an episode
// does not know what the episode is called, and nothing counts against it.
func TestRecordReturnsTheStateItWroteIncludingTheIncident(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.For = 0
	created, _ = s.Update(t.Context(), created, "")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}

	written, err := s.Record(t.Context(), firing(created, prev, storeNow, alerting.True))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if written.IncidentID == "" {
		t.Fatal("the opened incident's id did not come back")
	}
	if written.Phase != alerting.PhaseFiring {
		t.Errorf("phase %s, want firing", written.Phase)
	}

	// And a notification against it counts.
	if err := s.RecordNotification(t.Context(), created.ID, written.IncidentID, storeNow); err != nil {
		t.Fatalf("record notification: %v", err)
	}
	incidents, err := s.Incidents(t.Context(), alerting.IncidentFilter{WatchID: created.ID})
	if err != nil {
		t.Fatalf("incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("%d incidents, want 1", len(incidents))
	}
	if incidents[0].Notifications != 1 {
		t.Errorf("the episode counted %d notifications, want 1", incidents[0].Notifications)
	}
}

// Disabling a watch has to close whatever it had open: an episode that outlives
// the watch that could resolve it stays open forever, and somebody switching a
// noisy watch off means to stop hearing about it rather than to freeze its last
// incident on the dashboard.
func TestSavingADisabledWatchRetiresIt(t *testing.T) {
	s := newStore(t)
	created := mustCreate(t, s, "checkout errors")
	created.For = 0
	created, _ = s.Update(t.Context(), created, "")
	prev := alerting.State{WatchID: created.ID, Phase: alerting.PhaseOK, DefinitionHash: created.DefinitionHash}
	if _, err := s.Record(t.Context(), firing(created, prev, storeNow, alerting.True)); err != nil {
		t.Fatalf("record: %v", err)
	}

	svc := alerting.NewService(s, nil, nil)
	created.Enabled = false
	if _, err := svc.Update(t.Context(), created, ""); err != nil {
		t.Fatalf("disable: %v", err)
	}

	incidents, err := s.Incidents(t.Context(), alerting.IncidentFilter{WatchID: created.ID})
	if err != nil {
		t.Fatalf("incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].Open() {
		t.Fatalf("incidents = %+v, want the open one closed", incidents)
	}
	if incidents[0].ClosedReason != alerting.ClosedDisabled {
		t.Errorf("closed reason %q, want %q", incidents[0].ClosedReason, alerting.ClosedDisabled)
	}

	// And saving it again while still disabled is a harmless no-op rather than a
	// second close over the first one's reason.
	if _, err := svc.Update(t.Context(), created, ""); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	again, _ := s.Incidents(t.Context(), alerting.IncidentFilter{WatchID: created.ID})
	if again[0].ClosedReason != alerting.ClosedDisabled || !again[0].ResolvedAt.Equal(*incidents[0].ResolvedAt) {
		t.Errorf("a second save changed the closed episode: %+v", again[0])
	}
}
