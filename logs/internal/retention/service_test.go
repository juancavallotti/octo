package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/juancavallotti/octo/logs/internal/repo"
)

// fixedNow is the clock every service test runs against, so a cutoff is a value
// the assertions can name rather than a moving target.
var fixedNow = time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)

type fakeSettings struct {
	current stored
	saved   *stored
	getErr  error
	putErr  error
}

func (f *fakeSettings) Get(context.Context) (stored, error) { return f.current, f.getErr }

func (f *fakeSettings) Put(_ context.Context, s stored) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.saved = &s
	f.current = s
	return nil
}

type fakeLogPruner struct {
	calls   []time.Time
	deleted int64
	err     error
}

func (f *fakeLogPruner) Prune(_ context.Context, cutoff time.Time, _ int) (int64, error) {
	f.calls = append(f.calls, cutoff)
	return f.deleted, f.err
}

type fakeTracePruner struct {
	calls  []time.Time
	result repo.TracePruneResult
	err    error
}

func (f *fakeTracePruner) Prune(_ context.Context, cutoff time.Time, _ int) (repo.TracePruneResult, error) {
	f.calls = append(f.calls, cutoff)
	return f.result, f.err
}

// fakeLock records whether the sweep lock was taken and released, and can refuse
// to hand it over.
type fakeLock struct {
	held     bool
	released bool
	taken    bool
	err      error
}

func (f *fakeLock) tryLock(context.Context) (func(), bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	if f.held {
		return nil, false, nil
	}
	f.taken = true
	return func() { f.released = true }, true, nil
}

// newTestService builds a service over fakes, with the clock pinned.
func newTestService(policy stored) (*Service, *fakeSettings, *fakeLogPruner, *fakeTracePruner, *fakeLock) {
	settings := &fakeSettings{current: policy}
	logs := &fakeLogPruner{}
	traces := &fakeTracePruner{}
	lock := &fakeLock{}
	svc := &Service{
		settings: settings,
		logs:     logs,
		traces:   traces,
		lock:     lock,
		batch:    10,
		now:      func() time.Time { return fixedNow },
	}
	return svc, settings, logs, traces, lock
}

func TestCutoff(t *testing.T) {
	if _, ok := cutoff(0, fixedNow); ok {
		t.Fatal("zero days reported an axis to prune")
	}
	if _, ok := cutoff(-3, fixedNow); ok {
		t.Fatal("a negative window reported an axis to prune")
	}
	got, ok := cutoff(30, fixedNow)
	if !ok {
		t.Fatal("30 days reported nothing to prune")
	}
	if want := fixedNow.AddDate(0, 0, -30); !got.Equal(want) {
		t.Fatalf("cutoff = %v, want %v", got, want)
	}
}

// The whole point of defaulting to zero is that installing the feature deletes
// nothing. A sweep under an unconfigured policy must not even reach for the lock.
func TestRunUnderAnEmptyPolicyDoesNothing(t *testing.T) {
	svc, _, logs, traces, lock := newTestService(stored{})

	result, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.LogsDeleted != 0 || result.TracesDeleted != 0 || result.TraceSummariesDeleted != 0 {
		t.Fatalf("result = %+v, want nothing deleted", result)
	}
	if result.LogsCutoff != nil || result.TracesCutoff != nil {
		t.Fatal("an unconfigured policy reported cutoffs")
	}
	if len(logs.calls) != 0 || len(traces.calls) != 0 {
		t.Fatal("an unconfigured policy still called a pruner")
	}
	if lock.taken {
		t.Fatal("an unconfigured policy took the sweep lock")
	}
}

// The two windows are independent, and a zero one has to mean "leave this stream
// alone" rather than "delete all of it".
func TestRunSkipsTheDisabledAxis(t *testing.T) {
	svc, _, logs, traces, _ := newTestService(stored{LogsDays: 30})
	logs.deleted = 12

	result, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.LogsDeleted != 12 {
		t.Fatalf("logs deleted = %d, want 12", result.LogsDeleted)
	}
	if len(traces.calls) != 0 {
		t.Fatalf("traces pruned at %v despite a zero window", traces.calls)
	}
	if result.TracesCutoff != nil {
		t.Fatalf("traces cutoff = %v, want nil", result.TracesCutoff)
	}
	if want := fixedNow.AddDate(0, 0, -30); result.LogsCutoff == nil || !result.LogsCutoff.Equal(want) {
		t.Fatalf("logs cutoff = %v, want %v", result.LogsCutoff, want)
	}
}

func TestRunPrunesBothStreamsAtTheirOwnCutoffs(t *testing.T) {
	svc, _, logs, traces, lock := newTestService(stored{LogsDays: 30, TracesDays: 7})
	logs.deleted = 100
	traces.result = repo.TracePruneResult{Records: 40, Summaries: 5}

	result, err := svc.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.LogsDeleted != 100 || result.TracesDeleted != 40 || result.TraceSummariesDeleted != 5 {
		t.Fatalf("result = %+v", result)
	}
	if len(logs.calls) != 1 || !logs.calls[0].Equal(fixedNow.AddDate(0, 0, -30)) {
		t.Fatalf("logs pruned at %v", logs.calls)
	}
	if len(traces.calls) != 1 || !traces.calls[0].Equal(fixedNow.AddDate(0, 0, -7)) {
		t.Fatalf("traces pruned at %v", traces.calls)
	}
	if !lock.released {
		t.Fatal("the sweep lock was not released")
	}
}

// The nightly job and somebody pressing Run now must not overlap.
func TestRunRefusesWhenASweepHoldsTheLock(t *testing.T) {
	svc, _, logs, traces, lock := newTestService(stored{LogsDays: 30})
	lock.held = true

	_, err := svc.Run(context.Background())
	if !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("err = %v, want ErrRunInProgress", err)
	}
	if len(logs.calls) != 0 || len(traces.calls) != 0 {
		t.Fatal("a refused sweep still pruned")
	}
}

// A failure partway through still has to report what it managed to delete, and
// still has to give the lock back — otherwise one broken sweep blocks every one
// after it.
func TestRunReportsPartialProgressAndReleasesTheLock(t *testing.T) {
	svc, _, logs, traces, lock := newTestService(stored{LogsDays: 30, TracesDays: 7})
	logs.deleted = 9
	logs.err = errors.New("connection reset")

	result, err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("run reported success despite a failed prune")
	}
	if result.LogsDeleted != 9 {
		t.Fatalf("logs deleted = %d, want the 9 that went before the failure", result.LogsDeleted)
	}
	if len(traces.calls) != 0 {
		t.Fatal("the sweep carried on to traces after logs failed")
	}
	if !lock.released {
		t.Fatal("a failed sweep kept the lock")
	}
}

func TestUpdateStampsAndStores(t *testing.T) {
	svc, settings, _, _, _ := newTestService(stored{})

	policy, err := svc.Update(context.Background(), Update{LogsDays: 30, TracesDays: 7})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if settings.saved == nil {
		t.Fatal("nothing was written")
	}
	if settings.saved.LogsDays != 30 || settings.saved.TracesDays != 7 {
		t.Fatalf("saved = %+v", settings.saved)
	}
	if !settings.saved.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("updatedAt = %v, want %v", settings.saved.UpdatedAt, fixedNow)
	}
	if policy.UpdatedAt == nil || !policy.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("returned policy carries updatedAt = %v", policy.UpdatedAt)
	}
}

func TestUpdateRejectsAnImpossibleWindowWithoutWriting(t *testing.T) {
	svc, settings, _, _, _ := newTestService(stored{LogsDays: 30})

	_, err := svc.Update(context.Background(), Update{LogsDays: -1})
	if !errors.Is(err, ErrInvalidDays) {
		t.Fatalf("err = %v, want ErrInvalidDays", err)
	}
	if settings.saved != nil {
		t.Fatalf("a rejected update was written: %+v", settings.saved)
	}
}

func TestGetReportsTheStoredPolicy(t *testing.T) {
	when := fixedNow.Add(-time.Hour)
	svc, _, _, _, _ := newTestService(stored{LogsDays: 14, TracesDays: 3, UpdatedAt: when})

	policy, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if policy.LogsDays != 14 || policy.TracesDays != 3 {
		t.Fatalf("policy = %+v", policy)
	}
	if policy.UpdatedAt == nil || !policy.UpdatedAt.Equal(when) {
		t.Fatalf("updatedAt = %v, want %v", policy.UpdatedAt, when)
	}
}
