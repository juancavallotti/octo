package alerting

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	// tickInterval is how often the runner looks for work. It is not a watch's
	// interval — a watch decides its own — it is the resolution at which the
	// scheduler notices one has come due, and it matches the shortest interval a
	// watch may ask for.
	tickInterval = MinInterval

	// maxConcurrentWatches bounds how many watches one tick evaluates at once.
	// Four concurrent aggregate queries is a load the trace table already carries
	// from the UI, and this pool is shared with the ingest path — a watch is
	// never urgent enough to compete with the records it is about.
	maxConcurrentWatches = 4

	// evalTimeout bounds one watch. Generous enough for a percentile over an hour
	// of summaries, short enough that a stuck query cannot hold the tick open.
	evalTimeout = 30 * time.Second

	// maxDuePerTick caps one pass. A backlog is drained over several ticks rather
	// than in one long pass that holds the pool.
	maxDuePerTick = 50

	// invalidRetry is how long a watch whose definition will not build is parked
	// for. Long, because the fix is a human editing it, and a definition this
	// process cannot read should not consume a slot every minute in the meantime.
	invalidRetry = time.Hour

	// ingestGrace is how far back the no-ingest guard looks. Two buckets at the
	// shortest step, so a momentary gap between two records does not read as a
	// dead pipeline.
	ingestGrace = 2 * MinStep
)

// The interfaces the runner consumes, declared here where they are used.
//
// The store one is larger than this codebase usually likes. It is one collaborator
// with one job — everything the scheduler does to a row — and splitting it into a
// due-lister, a recorder and a parker would be three names for the same thing and
// three fakes in every test that drives a tick.
type store interface {
	Due(ctx context.Context, now time.Time, limit int) ([]Due, error)
	// Record returns the state it actually wrote, because the incident id is
	// minted during the write: the runner asks for an episode to be opened and
	// only the store knows what it was called.
	Record(ctx context.Context, r Result) (State, error)
	RecordNotification(ctx context.Context, watchID, incidentID string, at time.Time) error
	Defer(ctx context.Context, watchID string, until time.Time) error
	MarkInvalid(ctx context.Context, watchID string, until time.Time, reason string) error
}

type fetcher interface {
	Fetch(ctx context.Context, q Query) (Series, error)
	// Ingesting reports whether anything at all has arrived recently. One cheap
	// query per tick, shared by every watch.
	Ingesting(ctx context.Context, since time.Time) (bool, error)
}

type elector interface {
	IsLeader() bool
}

type notifier interface {
	Notify(ctx context.Context, w Watch, n Notification) []DeliveryResult
}

// DeliveryResult is one action's outcome, as the notifier reports it.
type DeliveryResult struct {
	ActionID string `json:"actionId"`
	Type     string `json:"type"`
	Err      string `json:"error,omitempty"`
}

// Delivered reports whether the action went through.
func (r DeliveryResult) Delivered() bool { return r.Err == "" }

// Runner evaluates every due watch, on the leader only.
type Runner struct {
	store    store
	fetch    fetcher
	leader   elector
	notify   notifier
	interval time.Duration
	now      func() time.Time
}

// NewRunner wires a runner over its collaborators.
func NewRunner(s store, f fetcher, leader elector, n notifier) *Runner {
	return &Runner{store: s, fetch: f, leader: leader, notify: n, interval: tickInterval, now: time.Now}
}

// Run evaluates due watches until ctx is done.
//
// It ticks once immediately rather than waiting out the first interval, on the
// same terms the price refresher does: a process that has just taken the lease
// has no reason to leave the installation unwatched for a minute.
func (r *Runner) Run(ctx context.Context) {
	r.tick(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick evaluates whatever is due.
//
// A replica that is not the leader returns immediately. It does not wait to
// become one: the tick it would be waiting for has already been served by the
// replica that is.
func (r *Runner) tick(ctx context.Context) {
	if !r.leader.IsLeader() {
		return
	}
	now := r.now()
	due, err := r.store.Due(ctx, now, maxDuePerTick)
	if err != nil {
		slog.Error("could not list due watches", "error", err)
		return
	}
	if len(due) == 0 {
		return
	}

	ingesting := r.ingesting(ctx, now)
	r.evaluateAll(ctx, due, now, ingesting)
}

// ingesting answers the one shared question a tick asks before it starts.
//
// A pipeline that has stopped delivering looks exactly like an installation with
// no traffic, and every downward condition — an absence, a spike down, a
// threshold below — is satisfied by both. So when nothing at all has arrived
// recently those watches are recorded as skipped rather than evaluated, and the
// whole install does not page over one broken consumer.
//
// A failure here is read as "yes". Suppressing every downward watch because a
// cheap probe failed would be the same outage in the other direction.
func (r *Runner) ingesting(ctx context.Context, now time.Time) bool {
	ok, err := r.fetch.Ingesting(ctx, now.Add(-ingestGrace))
	if err != nil {
		slog.Warn("could not tell whether telemetry is still arriving; "+
			"evaluating downward watches anyway", "error", err)
		return true
	}
	if !ok {
		slog.Warn("nothing has been ingested recently; "+
			"watches that fire on an absence are skipped this tick", "since", now.Add(-ingestGrace))
	}
	return ok
}

// evaluateAll runs the due watches through a bounded pool, so one slow watch
// cannot stall the tick and a backlog cannot flood the connection pool.
func (r *Runner) evaluateAll(ctx context.Context, due []Due, now time.Time, ingesting bool) {
	sem := make(chan struct{}, maxConcurrentWatches)
	var wg sync.WaitGroup
	for _, item := range due {
		wg.Add(1)
		go func(item Due) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.evaluateOne(ctx, item, now, ingesting)
		}(item)
	}
	wg.Wait()
}

// evaluateOne evaluates a single watch and records what it decided.
func (r *Runner) evaluateOne(ctx context.Context, item Due, now time.Time, ingesting bool) {
	built, err := Build(item.Watch)
	if err != nil {
		// A definition this process cannot read is parked and said so, never
		// evaluated in part. Which conditions were dropped would change what
		// `all` and `any` mean, in opposite directions.
		if err := r.store.MarkInvalid(ctx, item.Watch.ID, now.Add(invalidRetry), err.Error()); err != nil {
			slog.Error("could not park an invalid watch", "watch", item.Watch.Name, "error", err)
		}
		return
	}
	if !ingesting && built.Downward() {
		if err := r.store.Defer(ctx, item.Watch.ID, now.Add(item.Watch.Interval)); err != nil {
			slog.Error("could not defer a watch", "watch", item.Watch.Name, "error", err)
		}
		return
	}

	evalCtx, cancel := context.WithTimeout(ctx, evalTimeout)
	defer cancel()

	started := r.now()
	evaluation := built.Evaluate(now, r.fetchAll(evalCtx, built, now))
	next, actions := Step(item.State, item.Watch, evaluation, now)
	result := Result{
		Watch: item.Watch, Evaluation: evaluation,
		Previous: item.State, State: next, Actions: actions,
		Duration: r.now().Sub(started),
	}

	recorded, err := r.store.Record(ctx, result)
	if err != nil {
		if errors.Is(err, ErrStaleEvaluation) {
			// Another evaluator got there first, which means the lease has moved
			// and its decision is the newer one. Dropped rather than retried.
			slog.Warn("a newer evaluation is already recorded; dropping this one",
				"watch", item.Watch.Name)
			return
		}
		slog.Error("could not record an evaluation", "watch", item.Watch.Name, "error", err)
		return
	}
	r.announce(ctx, item, result, recorded)
}

// fetchAll runs a watch's plan.
//
// Sequentially, and deliberately: the plan is already coalesced down to one query
// per distinct set of rows, the whole tick is running several watches at once,
// and a second layer of concurrency inside each would multiply the load on a pool
// this service shares with ingest.
func (r *Runner) fetchAll(ctx context.Context, built *Built, now time.Time) map[string]Fetched {
	plan := built.Plan(now)
	out := make(map[string]Fetched, len(plan))
	for _, q := range plan {
		series, err := r.fetch.Fetch(ctx, q)
		out[q.Key()] = Fetched{Series: series, Err: err}
	}
	return out
}

// announce delivers whatever the state machine asked for.
//
// A delivery failure never rolls back the transition that has already been
// recorded. The watch did fire, the incident is open, and losing that fact
// because a mailer was down is strictly the worse failure — the next renotify
// tries again.
func (r *Runner) announce(ctx context.Context, item Due, result Result, next State) {
	if len(result.Actions) == 0 || r.notify == nil {
		return
	}
	if next.Muted(result.Evaluation.At) {
		slog.Debug("watch is muted; not announcing", "watch", item.Watch.Name)
		return
	}
	for _, action := range result.Actions {
		notification := NewNotification(item.Watch, next, result.Evaluation, action)
		delivered := r.notify.Notify(ctx, item.Watch, notification)
		if !anyDelivered(delivered) {
			continue
		}
		// Only a delivery that actually reached somebody restarts the renotify
		// cooldown. Stamping it on an attempt would silence a watch for the whole
		// interval because a mailer was briefly down.
		if err := r.store.RecordNotification(ctx, item.Watch.ID, next.IncidentID, action.At); err != nil {
			slog.Error("could not record a notification", "watch", item.Watch.Name, "error", err)
		}
	}
}

func anyDelivered(results []DeliveryResult) bool {
	for _, r := range results {
		if r.Delivered() {
			return true
		}
	}
	return false
}

// Preview evaluates a watch now without recording anything or telling anybody.
//
// This is what makes the condition vocabulary usable: an author sees every
// condition's observed value against its threshold before saving, so "why does
// this not fire" is answered in the editor rather than in production. It is the
// same plan, fetch and combine the runner uses, with the state machine skipped —
// which is only possible because those three are pure.
func (r *Runner) Preview(ctx context.Context, w Watch) (Evaluation, error) {
	built, err := Build(w)
	if err != nil {
		return Evaluation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, evalTimeout)
	defer cancel()

	now := r.now()
	return built.Evaluate(now, r.fetchAll(ctx, built, now)), nil
}
