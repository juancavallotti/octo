package alerting

import (
	"testing"
	"time"
)

// driver runs a sequence of evaluations through Step, which is the only way to
// test a machine whose whole job is what happens on the third one.
type driver struct {
	t       *testing.T
	watch   Watch
	state   State
	now     time.Time
	actions []Action
}

func newDriver(t *testing.T, w Watch) *driver {
	t.Helper()
	w.ID = "w_1"
	w.DefinitionHash = "h1"
	return &driver{
		t:     t,
		watch: w,
		state: State{WatchID: w.ID, Phase: PhaseOK, DefinitionHash: "h1"},
		now:   fixtureStart,
	}
}

// tick advances one interval and applies an evaluation with the given verdict.
func (d *driver) tick(verdict Truth) *driver {
	d.t.Helper()
	d.now = d.now.Add(d.watch.Interval)
	status := StatusOK
	switch verdict {
	case True:
		status = StatusFiring
	case Unknown:
		status = StatusInsufficient
	}
	ev := Evaluation{At: d.now, Verdict: verdict, Status: status, Total: 1}
	var actions []Action
	d.state, actions = Step(d.state, d.watch, ev, d.now)
	d.actions = actions
	// The store writes an incident id back when it opens one; the machine only
	// asks for it, so the driver stands in for that here.
	for _, a := range actions {
		if a.Kind == ActionOpen {
			d.state.IncidentID = "i_1"
			d.state.LastNotifiedAt = d.now
		}
		if a.Kind == ActionRenotify {
			d.state.LastNotifiedAt = d.now
		}
	}
	return d
}

func (d *driver) wantPhase(want Phase) *driver {
	d.t.Helper()
	if d.state.Phase != want {
		d.t.Fatalf("phase %s, want %s (firing streak %d, ok streak %d)",
			d.state.Phase, want, d.state.ConsecutiveFiring, d.state.ConsecutiveOK)
	}
	return d
}

func (d *driver) wantActions(want ...ActionKind) *driver {
	d.t.Helper()
	if len(d.actions) != len(want) {
		d.t.Fatalf("actions %v, want %v", kinds(d.actions), want)
	}
	for i, k := range want {
		if d.actions[i].Kind != k {
			d.t.Fatalf("actions %v, want %v", kinds(d.actions), want)
		}
	}
	return d
}

func kinds(actions []Action) []ActionKind {
	out := make([]ActionKind, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.Kind)
	}
	return out
}

func held(forDur time.Duration) Watch {
	return Watch{Interval: time.Minute, For: forDur, Combinator: CombineAll}
}

func TestHoldRequiresConsecutiveEvaluations(t *testing.T) {
	d := newDriver(t, held(3*time.Minute))
	d.tick(True).wantPhase(PhasePending).wantActions()
	d.tick(True).wantPhase(PhasePending).wantActions()
	d.tick(True).wantPhase(PhaseFiring).wantActions(ActionOpen)
	// Already firing: no second announcement without a renotify interval.
	d.tick(True).wantPhase(PhaseFiring).wantActions()
}

func TestAHoleInTheHoldIsNotAHold(t *testing.T) {
	d := newDriver(t, held(3*time.Minute))
	d.tick(True).tick(True).wantPhase(PhasePending)
	// One evaluation that could not decide, and the streak is gone. It must not
	// count as part of three minutes of firing.
	d.tick(Unknown).wantPhase(PhaseOK).wantActions()
	d.tick(True).wantPhase(PhasePending)
	d.tick(True).wantPhase(PhasePending)
	d.tick(True).wantPhase(PhaseFiring).wantActions(ActionOpen)
}

func TestAFalseDuringTheHoldReturnsToRest(t *testing.T) {
	d := newDriver(t, held(3*time.Minute))
	d.tick(True).tick(True).wantPhase(PhasePending)
	d.tick(False).wantPhase(PhaseOK).wantActions()
}

// Resolution is asymmetric on purpose: one clean sample is not recovery, and a
// metric hovering at its threshold would otherwise open and resolve every minute.
func TestResolutionNeedsMoreThanOneCleanEvaluation(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantPhase(PhaseFiring).wantActions(ActionOpen)
	d.tick(False).wantPhase(PhaseFiring).wantActions()
	d.tick(False).wantPhase(PhaseOK).wantActions(ActionResolve)
	if d.actions[0].Reason != ClosedResolved {
		t.Errorf("closed reason %q, want %q", d.actions[0].Reason, ClosedResolved)
	}
}

func TestFlappingAtTheThresholdDoesNotEmitAPairPerMinute(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantActions(ActionOpen)
	// Alternating true/false forever must produce nothing further: each lone
	// clean evaluation is undone by the next firing one before it can resolve.
	for range 10 {
		d.tick(False).wantPhase(PhaseFiring).wantActions()
		d.tick(True).wantPhase(PhaseFiring).wantActions()
	}
}

// The rule that stops an outage being recorded as fixed because the pipeline
// stopped reporting.
func TestAnIncidentIsNeverResolvedByAnAbsenceOfEvidence(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantActions(ActionOpen)
	for range staleEvaluations - 1 {
		d.tick(Unknown).wantPhase(PhaseFiring).wantActions()
	}
	d.tick(Unknown).wantPhase(PhaseOK).wantActions(ActionClose)
	if got := d.actions[0].Reason; got != ClosedStale {
		t.Errorf("closed reason %q, want %q — a stale episode must not read as recovered", got, ClosedStale)
	}
}

// An undecided evaluation in the middle of an outage must not count toward
// recovery either: it resets nothing about the clean streak it never joined.
func TestUndecidedDoesNotAdvanceRecovery(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantActions(ActionOpen)
	d.tick(False).wantPhase(PhaseFiring)
	d.tick(Unknown).wantPhase(PhaseFiring).wantActions()
	// The clean streak was interrupted, so this single clean evaluation is not
	// yet enough.
	d.tick(False).wantPhase(PhaseFiring).wantActions()
	d.tick(False).wantPhase(PhaseOK).wantActions(ActionResolve)
}

func TestRenotifyRespectsItsCooldown(t *testing.T) {
	w := held(time.Minute)
	w.Renotify = 5 * time.Minute
	d := newDriver(t, w)
	d.tick(True).wantActions(ActionOpen)
	for range 4 {
		d.tick(True).wantActions()
	}
	d.tick(True).wantActions(ActionRenotify)
	d.tick(True).wantActions()
}

func TestNoRenotifyIntervalAnnouncesOnce(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantActions(ActionOpen)
	for range 20 {
		d.tick(True).wantActions()
	}
}

// A retuned threshold restarts the hold, and closes any episode it was already
// running, because the episode was about a question that no longer exists.
func TestADefinitionChangeResetsTheMachine(t *testing.T) {
	d := newDriver(t, held(3*time.Minute))
	d.tick(True).tick(True).wantPhase(PhasePending)

	d.watch.DefinitionHash = "h2"
	d.tick(True).wantPhase(PhasePending)
	if d.state.ConsecutiveFiring != 1 {
		t.Errorf("firing streak %d, want 1 — the hold carried across an edit", d.state.ConsecutiveFiring)
	}
	if d.state.DefinitionHash != "h2" {
		t.Errorf("state hash %q, want h2", d.state.DefinitionHash)
	}
}

func TestADefinitionChangeClosesAnOpenEpisode(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantActions(ActionOpen)

	d.watch.DefinitionHash = "h2"
	d.tick(True)
	if len(d.actions) == 0 || d.actions[0].Kind != ActionClose || d.actions[0].Reason != ClosedEdited {
		t.Fatalf("actions %v, want a close for %q", kinds(d.actions), ClosedEdited)
	}
	// And it starts a fresh hold rather than continuing the old episode.
	if d.state.Phase != PhaseFiring || d.state.ConsecutiveFiring != 1 {
		t.Errorf("phase %s streak %d, want firing/1", d.state.Phase, d.state.ConsecutiveFiring)
	}
}

// An incident that outlives its watch has nothing left that could ever resolve it.
func TestRetireClosesAnOpenEpisode(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(True).wantActions(ActionOpen)

	state, actions := Retire(d.state, d.watch, d.now, ClosedDisabled)
	if len(actions) != 1 || actions[0].Kind != ActionClose || actions[0].Reason != ClosedDisabled {
		t.Fatalf("actions %v, want a close for %q", kinds(actions), ClosedDisabled)
	}
	if state.Phase != PhaseOK || state.IncidentID != "" {
		t.Errorf("phase %s incident %q, want ok and no incident", state.Phase, state.IncidentID)
	}
}

func TestRetireOnAQuietWatchAsksForNothing(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	if _, actions := Retire(d.state, d.watch, d.now, ClosedDeleted); len(actions) != 0 {
		t.Errorf("actions %v, want none — there was no episode to close", kinds(actions))
	}
}

func TestStepSchedulesTheNextEvaluation(t *testing.T) {
	d := newDriver(t, held(time.Minute))
	d.tick(False)
	if want := d.now.Add(time.Minute); !d.state.NextDueAt.Equal(want) {
		t.Errorf("next due %s, want %s", d.state.NextDueAt, want)
	}
	// Scheduled from now, not from the last due time: after an outage that is
	// what resumes rather than replaying a backlog of windows nobody is waiting for.
	if d.state.NextDueAt.Before(d.now) {
		t.Error("the next evaluation was scheduled in the past")
	}
}

func TestStepRecordsWhatItSaw(t *testing.T) {
	w := held(time.Minute)
	w.ID = "w_1"
	w.DefinitionHash = "h1"
	observed := 41.0
	ev := Evaluation{
		At: fixtureStart, Verdict: True, Status: StatusFiring, Total: 2, Matched: 2,
		Outcomes: []Outcome{{ConditionID: "c_1"}, {ConditionID: "c_2", Observed: &observed}},
	}
	state, _ := Step(State{WatchID: "w_1", Phase: PhaseOK, DefinitionHash: "h1"}, w, ev, fixtureStart)

	if state.LastStatus != StatusFiring {
		t.Errorf("last status %s, want firing", state.LastStatus)
	}
	if state.LastValue == nil || *state.LastValue != observed {
		t.Errorf("last value %v, want the first observed number %v", state.LastValue, observed)
	}
	if !state.LastEvalAt.Equal(fixtureStart) {
		t.Error("the evaluation time was not recorded")
	}
}

func TestMuted(t *testing.T) {
	now := fixtureStart
	if (State{}).Muted(now) {
		t.Error("a watch with no mute reported itself muted")
	}
	if !(State{MutedUntil: now.Add(time.Hour)}).Muted(now) {
		t.Error("a muted watch did not report itself muted")
	}
	if (State{MutedUntil: now.Add(-time.Hour)}).Muted(now) {
		t.Error("an expired mute is still muting")
	}
}
