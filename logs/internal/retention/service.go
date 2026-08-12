package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/logs/internal/repo"
)

// pruneBatch is how many rows one delete statement removes before committing and
// going round again. Large enough that a sweep of millions of rows is not
// thousands of round trips, small enough that each statement is over quickly and
// leaves the ingest path alone between them.
const pruneBatch = 5000

// The interfaces below are declared here, where they are consumed, and kept to
// what this service actually calls. The repos satisfy them; a test does not need
// a database to check that a disabled axis is skipped or that a second sweep is
// refused.

type settingsRepo interface {
	Get(ctx context.Context) (stored, error)
	Put(ctx context.Context, s stored) error
}

type logPruner interface {
	Prune(ctx context.Context, cutoff time.Time, batch int) (int64, error)
}

type tracePruner interface {
	Prune(ctx context.Context, cutoff time.Time, batch int) (repo.TracePruneResult, error)
}

type locker interface {
	tryLock(ctx context.Context) (func(), bool, error)
}

// Service reads and writes the retention policy, and enforces it on demand.
type Service struct {
	settings settingsRepo
	logs     logPruner
	traces   tracePruner
	lock     locker
	batch    int
	now      func() time.Time
}

// NewService wires the policy and both pruners onto one pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		settings: NewRepo(pool),
		logs:     repo.NewLogs(pool),
		traces:   repo.NewTraces(pool),
		lock:     &sweepLock{pool: pool},
		batch:    pruneBatch,
		now:      time.Now,
	}
}

// Get returns the stored policy.
func (s *Service) Get(ctx context.Context) (Policy, error) {
	current, err := s.settings.Get(ctx)
	if err != nil {
		return Policy{}, err
	}
	return current.toPolicy(), nil
}

// Update validates and stores a policy, returning what was written.
//
// A save replaces the row outright rather than merging into it: both windows are
// always supplied, so there is no stored field a save could be carrying forward
// and no reason to read before writing.
func (s *Service) Update(ctx context.Context, u Update) (Policy, error) {
	if err := validateUpdate(u); err != nil {
		return Policy{}, err
	}
	next := stored{
		LogsDays:   u.LogsDays,
		TracesDays: u.TracesDays,
		UpdatedAt:  s.now().UTC(),
	}
	if err := s.settings.Put(ctx, next); err != nil {
		return Policy{}, err
	}
	return next.toPolicy(), nil
}

// RunResult reports one sweep. The cutoffs are nil for an axis that is set to
// keep everything, which is what tells a reader that a zero count means "nothing
// was asked for" rather than "nothing had expired".
type RunResult struct {
	LogsDeleted           int64
	TracesDeleted         int64
	TraceSummariesDeleted int64
	LogsCutoff            *time.Time
	TracesCutoff          *time.Time
	Duration              time.Duration
}

// Run enforces the stored policy now.
//
// A policy that keeps everything returns an empty result rather than an error:
// being asked to do nothing is a state this reports, not one it refuses. A sweep
// already in progress is refused with ErrRunInProgress, so the nightly job and
// somebody pressing the button cannot overlap.
func (s *Service) Run(ctx context.Context) (RunResult, error) {
	policy, err := s.Get(ctx)
	if err != nil {
		return RunResult{}, err
	}

	started := s.now()
	out := RunResult{}
	if !policy.Enabled() {
		return out, nil
	}

	// Taken after the policy read so a disabled policy costs no connection and
	// cannot be reported as a conflict with a sweep that has nothing to do.
	release, acquired, err := s.lock.tryLock(ctx)
	if err != nil {
		return out, err
	}
	if !acquired {
		return out, ErrRunInProgress
	}
	defer release()

	if cut, ok := cutoff(policy.LogsDays, started); ok {
		out.LogsCutoff = &cut
		deleted, err := s.logs.Prune(ctx, cut, s.batch)
		out.LogsDeleted = deleted
		if err != nil {
			return out, err
		}
	}

	if cut, ok := cutoff(policy.TracesDays, started); ok {
		out.TracesCutoff = &cut
		result, err := s.traces.Prune(ctx, cut, s.batch)
		out.TracesDeleted = result.Records
		out.TraceSummariesDeleted = result.Summaries
		if err != nil {
			return out, err
		}
	}

	out.Duration = s.now().Sub(started)
	slog.Info("retention sweep complete",
		"logs_deleted", out.LogsDeleted,
		"traces_deleted", out.TracesDeleted,
		"trace_summaries_deleted", out.TraceSummariesDeleted,
		"duration", out.Duration)
	return out, nil
}

// cutoff returns the instant before which records on an axis have expired, and
// whether the axis prunes at all.
//
// AddDate rather than a multiple of 24h, so a window is the number of calendar
// days it says it is on an installation whose clock observes them.
func cutoff(days int, now time.Time) (time.Time, bool) {
	if days <= 0 {
		return time.Time{}, false
	}
	return now.AddDate(0, 0, -days), true
}
