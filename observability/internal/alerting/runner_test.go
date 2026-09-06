package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// The fakes. The whole point of the runner's four consumed interfaces is that a
// tick can be driven without a database, a broker or a cluster.

type fakeStore struct {
	mu        sync.Mutex
	due       []Due
	recorded  []Result
	deferred  []string
	invalid   map[string]string
	notified  []string
	recordErr error
}

func newFakeStore(due ...Due) *fakeStore {
	return &fakeStore{due: due, invalid: map[string]string{}}
}

func (f *fakeStore) Due(context.Context, time.Time, int) ([]Due, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.due, nil
}

func (f *fakeStore) Record(_ context.Context, r Result) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return State{}, f.recordErr
	}
	f.recorded = append(f.recorded, r)
	// Standing in for the store, which mints an incident id during the write.
	written := r.State
	for _, a := range r.Actions {
		if a.Kind == ActionOpen {
			written.IncidentID = "i_" + r.Watch.ID
		}
	}
	return written, nil
}

func (f *fakeStore) RecordNotification(_ context.Context, watchID, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notified = append(f.notified, watchID)
	return nil
}

func (f *fakeStore) Defer(_ context.Context, watchID string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deferred = append(f.deferred, watchID)
	return nil
}

func (f *fakeStore) MarkInvalid(_ context.Context, watchID string, _ time.Time, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalid[watchID] = reason
	return nil
}

type fakeFetcher struct {
	mu        sync.Mutex
	series    Series
	err       error
	fetches   int
	ingesting bool
	ingestErr error
	block     chan struct{}
}

func (f *fakeFetcher) Fetch(ctx context.Context, _ Query) (Series, error) {
	f.mu.Lock()
	f.fetches++
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return Series{}, ctx.Err()
		}
	}
	return f.series, f.err
}

func (f *fakeFetcher) Ingesting(context.Context, time.Time) (bool, error) {
	return f.ingesting, f.ingestErr
}

func (f *fakeFetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}

type fakeElector struct{ leader bool }

func (f *fakeElector) IsLeader() bool { return f.leader }

type fakeNotifier struct {
	mu       sync.Mutex
	sent     []Notification
	delivers bool
}

func (f *fakeNotifier) Notify(_ context.Context, _ Watch, n Notification) []DeliveryResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	if f.delivers {
		return []DeliveryResult{{ActionID: "a_1", Type: ActionTypeLog}}
	}
	return []DeliveryResult{{ActionID: "a_1", Type: ActionTypeEmail, Err: "the mailer is down"}}
}

// runnerWatch is a one-condition watch that fires whenever the series sums above
// the threshold, with no hold — so one tick is one decision.
func runnerWatch(t *testing.T, id string, threshold float64) Watch {
	t.Helper()
	return Watch{
		ID: id, Name: id, Enabled: true, Severity: "warning",
		Combinator: CombineAll, OnNoData: NoDataOK,
		Step: time.Minute, Interval: time.Minute, For: 0,
		DefinitionHash: "h1",
		Conditions:     []ConditionSpec{countThreshold(t, "c_1", threshold)},
		Actions:        []ActionSpec{{ID: "a_1", Type: ActionTypeLog, Params: json.RawMessage(`{}`)}},
	}
}

func dueWatch(t *testing.T, id string, threshold float64) Due {
	t.Helper()
	w := runnerWatch(t, id, threshold)
	return Due{Watch: w, State: State{WatchID: w.ID, Phase: PhaseOK, DefinitionHash: w.DefinitionHash}}
}

func newRunner(s store, f fetcher, leader bool, n notifier) *Runner {
	r := NewRunner(s, f, &fakeElector{leader: leader}, n)
	r.now = func() time.Time { return nowAfter(5, time.Minute) }
	return r
}

// A replica that is not the leader does nothing at all — it does not even ask
// what is due, because the tick it would be serving has already been served.
func TestANonLeaderDoesNothing(t *testing.T) {
	s := newFakeStore(dueWatch(t, "w_1", 1))
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: true}

	newRunner(s, f, false, &fakeNotifier{}).tick(t.Context())

	if f.count() != 0 {
		t.Errorf("a standby replica issued %d fetches", f.count())
	}
	if len(s.recorded) != 0 {
		t.Errorf("a standby replica recorded %d evaluations", len(s.recorded))
	}
}

func TestATickEvaluatesAndRecordsEveryDueWatch(t *testing.T) {
	s := newFakeStore(dueWatch(t, "w_1", 1), dueWatch(t, "w_2", 1000))
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: true} // sums to 15
	n := &fakeNotifier{delivers: true}

	newRunner(s, f, true, n).tick(t.Context())

	if len(s.recorded) != 2 {
		t.Fatalf("%d evaluations recorded, want 2", len(s.recorded))
	}
	byWatch := map[string]Result{}
	for _, r := range s.recorded {
		byWatch[r.Watch.ID] = r
	}
	if byWatch["w_1"].Evaluation.Status != StatusFiring {
		t.Errorf("w_1 status %s, want firing", byWatch["w_1"].Evaluation.Status)
	}
	if byWatch["w_2"].Evaluation.Status != StatusOK {
		t.Errorf("w_2 status %s, want ok", byWatch["w_2"].Evaluation.Status)
	}
	if len(n.sent) != 1 || n.sent[0].Kind != ActionOpen {
		t.Errorf("notifications = %+v, want one open for w_1", n.sent)
	}
	if len(s.notified) != 1 {
		t.Errorf("%d notifications recorded, want 1", len(s.notified))
	}
}

// A failed fetch becomes an undecided evaluation with a row of its own, not a
// dropped tick. A watch that stopped being evaluated must not look like one that
// found nothing.
func TestAFailedFetchIsRecordedRatherThanDropped(t *testing.T) {
	s := newFakeStore(dueWatch(t, "w_1", 1))
	f := &fakeFetcher{err: errors.New("postgres is unreachable"), ingesting: true}

	newRunner(s, f, true, &fakeNotifier{}).tick(t.Context())

	if len(s.recorded) != 1 {
		t.Fatalf("%d evaluations recorded, want 1", len(s.recorded))
	}
	got := s.recorded[0].Evaluation
	if got.Status != StatusError {
		t.Errorf("status %s, want error", got.Status)
	}
	if !got.Degraded || got.Outcomes[0].Err == "" {
		t.Errorf("the row does not say what went wrong: %+v", got)
	}
}

// A definition this process cannot read is parked and said so, never evaluated
// in part: which conditions were dropped would change what all and any mean.
func TestAnUnreadableDefinitionIsParked(t *testing.T) {
	broken := dueWatch(t, "w_1", 1)
	broken.Watch.Conditions[0].Type = "sorcery"
	s := newFakeStore(broken)
	f := &fakeFetcher{series: counts(1), ingesting: true}

	newRunner(s, f, true, &fakeNotifier{}).tick(t.Context())

	if len(s.recorded) != 0 || f.count() != 0 {
		t.Error("an unreadable watch was evaluated anyway")
	}
	if reason, ok := s.invalid["w_1"]; !ok || reason == "" {
		t.Errorf("the watch was not parked with a reason: %q", reason)
	}
}

// A dead pipeline looks exactly like an idle installation, and every downward
// condition is satisfied by both. Those watches are deferred, not evaluated.
func TestWatchesThatFireOnAnAbsenceAreSkippedWhileNothingArrives(t *testing.T) {
	upward := dueWatch(t, "w_up", 1)
	downward := dueWatch(t, "w_down", 1)
	downward.Watch.Conditions[0] = ConditionSpec{
		ID: "c_1", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
	}
	s := newFakeStore(upward, downward)
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: false}

	newRunner(s, f, true, &fakeNotifier{}).tick(t.Context())

	if len(s.deferred) != 1 || s.deferred[0] != "w_down" {
		t.Errorf("deferred %v, want only the downward watch", s.deferred)
	}
	if len(s.recorded) != 1 || s.recorded[0].Watch.ID != "w_up" {
		t.Errorf("recorded %d evaluations, want only the upward watch", len(s.recorded))
	}
}

// Suppressing every downward watch because a cheap probe failed would be the
// same outage in the other direction.
func TestAFailedIngestProbeDoesNotSuppressAnything(t *testing.T) {
	downward := dueWatch(t, "w_down", 1)
	downward.Watch.Conditions[0] = ConditionSpec{
		ID: "c_1", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
	}
	s := newFakeStore(downward)
	f := &fakeFetcher{series: counts(1), ingestErr: errors.New("no")}

	newRunner(s, f, true, &fakeNotifier{}).tick(t.Context())

	if len(s.deferred) != 0 {
		t.Errorf("a failed probe suppressed %v", s.deferred)
	}
	if len(s.recorded) != 1 {
		t.Errorf("%d evaluations recorded, want 1", len(s.recorded))
	}
}

// Another evaluator got there first, which means the lease has moved and its
// decision is the newer one. Dropped, never retried.
func TestALostRaceIsDroppedRatherThanRetried(t *testing.T) {
	s := newFakeStore(dueWatch(t, "w_1", 1))
	s.recordErr = ErrStaleEvaluation
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: true}
	n := &fakeNotifier{delivers: true}

	newRunner(s, f, true, n).tick(t.Context())

	if len(n.sent) != 0 {
		t.Errorf("a dropped evaluation still announced %d notifications", len(n.sent))
	}
}

// Only a delivery that actually reached somebody restarts the renotify
// cooldown. Stamping it on an attempt would silence a watch for a whole interval
// because a mailer was briefly down.
func TestAFailedDeliveryDoesNotStartTheCooldown(t *testing.T) {
	s := newFakeStore(dueWatch(t, "w_1", 1))
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: true}
	n := &fakeNotifier{delivers: false}

	newRunner(s, f, true, n).tick(t.Context())

	if len(n.sent) != 1 {
		t.Fatalf("%d notifications attempted, want 1", len(n.sent))
	}
	if len(s.notified) != 0 {
		t.Error("a failed delivery restarted the renotify cooldown")
	}
	// And the transition itself still stands: the watch did fire.
	if len(s.recorded) != 1 || s.recorded[0].State.Phase != PhaseFiring {
		t.Error("a failed delivery rolled back the transition that caused it")
	}
}

// Muting suppresses the announcement, not the evaluation: the history stays
// complete and the incident still opens and resolves.
func TestAMutedWatchIsEvaluatedButNotAnnounced(t *testing.T) {
	muted := dueWatch(t, "w_1", 1)
	muted.State.MutedUntil = nowAfter(500, time.Minute)
	s := newFakeStore(muted)
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: true}
	n := &fakeNotifier{delivers: true}

	newRunner(s, f, true, n).tick(t.Context())

	if len(s.recorded) != 1 {
		t.Errorf("a muted watch was not evaluated")
	}
	if len(n.sent) != 0 {
		t.Errorf("a muted watch announced %d notifications", len(n.sent))
	}
}

// One slow watch must not stall the tick past its own budget, and must not stop
// its siblings being evaluated.
func TestASlowWatchDoesNotHoldTheTickOpenForever(t *testing.T) {
	s := newFakeStore(dueWatch(t, "w_1", 1))
	f := &fakeFetcher{series: counts(1), ingesting: true, block: make(chan struct{})}
	r := newRunner(s, f, true, &fakeNotifier{})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); r.tick(ctx) }()

	// Cancelling stands in for the per-watch timeout, which is thirty seconds and
	// not worth waiting out in a test; the path it exercises is the same one.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the tick did not return after its work was cancelled")
	}
}

// Preview is what makes the vocabulary usable: the same plan, fetch and combine
// the runner uses, with the state machine skipped — and nothing written.
func TestPreviewDecidesWithoutRecordingOrAnnouncing(t *testing.T) {
	s := newFakeStore()
	f := &fakeFetcher{series: counts(1, 2, 3, 4, 5), ingesting: true}
	n := &fakeNotifier{delivers: true}
	r := newRunner(s, f, true, n)

	got, err := r.Preview(t.Context(), runnerWatch(t, "w_1", 1))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got.Status != StatusFiring || got.Matched != 1 {
		t.Errorf("evaluation = %+v, want a firing verdict", got)
	}
	if len(s.recorded) != 0 || len(n.sent) != 0 {
		t.Error("a preview wrote something or told somebody")
	}
}

func TestPreviewRefusesAWatchItCannotBuild(t *testing.T) {
	r := newRunner(newFakeStore(), &fakeFetcher{}, true, &fakeNotifier{})
	broken := runnerWatch(t, "w_1", 1)
	broken.Conditions[0].Type = "sorcery"

	if _, err := r.Preview(t.Context(), broken); !errors.Is(err, ErrUnknownCondition) {
		t.Errorf("error = %v, want ErrUnknownCondition", err)
	}
}
