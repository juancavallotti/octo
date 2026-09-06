// Package source turns an alerting query into a series, by asking whichever
// store holds the answer.
//
// It is a package of its own so that the alerting package's tests never link
// pgx or a Redis client. Everything worth arguing about in alerting — whether a
// change is a spike, whether a proportion is evidence, when a hold has been held
// — is decided over value types by pure functions, and keeping the only code
// that needs a server on this side of the boundary is what makes that testable.
//
// The dependency runs one way: this package imports alerting for Query and
// Series, and alerting knows nothing about this one. The runner declares the
// narrow interface it needs and *Fetcher happens to satisfy it, which is the same
// relationship retention.Service has with repo.
package source

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/observability/internal/alerting"
	"github.com/juancavallotti/octo/observability/internal/podstats"
)

// queryTimeout bounds one fetch. Generous enough for a percentile over an hour
// of trace summaries, short enough that a watch cannot hold a tick open while
// the ingest path waits behind it for the same pool.
const queryTimeout = 20 * time.Second

// Fetcher answers alerting queries from the stores this service owns.
//
// The pool may be nil, on the same terms the rest of the service treats it:
// without a database the trace and log sources report that they are unavailable,
// rather than the process refusing to start. Pod stats keep working, because they
// are in Redis, which this service will not start without.
type Fetcher struct {
	pool  *pgxpool.Pool
	stats *podstats.Service
}

// New returns a fetcher over the two stores.
func New(pool *pgxpool.Pool, stats *podstats.Service) *Fetcher {
	return &Fetcher{pool: pool, stats: stats}
}

// Fetch answers one query.
//
// A failure here is per query and therefore per backend, which is what lets a
// watch combining a trace clause with a pod-stat clause survive Redis being
// unreachable: the trace clause still produces a number and the three-valued
// combine decides what that is worth.
func (f *Fetcher) Fetch(ctx context.Context, q alerting.Query) (alerting.Series, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	switch q.Source {
	case alerting.SourceTraces:
		return f.fetchSQL(ctx, q, traceProjection, "trace_summaries", "started_at")
	case alerting.SourceLogs:
		return f.fetchSQL(ctx, q, logProjection, "logs", "ts")
	case alerting.SourcePodStats:
		return f.fetchPodStats(ctx, q)
	default:
		return alerting.Series{}, fmt.Errorf("source: %w: %q", alerting.ErrUnknownSource, q.Source)
	}
}

// projector renders the SELECT list for one metric and aggregate, and says
// whether the result carries a denominator.
type projector func(metric string, agg alerting.Aggregate) (selectList string, ratio bool, err error)

// bucketedSQL is the shape every Postgres source uses.
//
// date_bin against the epoch, which is exactly what alerting.AlignDown computes
// on the Go side, so a bucket built here and a bucket built there are the same
// bucket. Absent buckets simply do not come back as rows: the caller starts from
// an all-unknown series and writes what arrived, which is what makes "no rows" an
// absent bucket by default rather than a zero somebody has to remember to avoid.
const bucketedSQL = `
SELECT date_bin($1::interval, %s, TIMESTAMPTZ 'epoch') AS bucket,
       %s
  FROM %s
 WHERE %s >= $2 AND %s < $3%s
 GROUP BY 1`

func (f *Fetcher) fetchSQL(
	ctx context.Context, q alerting.Query, project projector, table, tsColumn string,
) (alerting.Series, error) {
	if f.pool == nil {
		return alerting.Series{}, fmt.Errorf("source: %s are unavailable: this process has no database", q.Source)
	}
	selectList, ratio, err := project(q.Metric, q.Aggregate)
	if err != nil {
		return alerting.Series{}, err
	}

	args := []any{intervalOf(q.Step), q.From, q.To}
	where, args := scopeSQL(q.Source, q.Scope, args)
	sql := fmt.Sprintf(bucketedSQL, tsColumn, selectList, table, tsColumn, tsColumn, where)

	rows, err := f.pool.Query(ctx, sql, args...)
	if err != nil {
		return alerting.Series{}, fmt.Errorf("source: query %s %s: %w", q.Source, q.Metric, err)
	}
	defer rows.Close()

	series := alerting.NewSeries(q.From, q.To, q.Step)
	for rows.Next() {
		if err := scanBucket(rows, &series, ratio); err != nil {
			return alerting.Series{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return alerting.Series{}, fmt.Errorf("source: read %s %s: %w", q.Source, q.Metric, err)
	}
	if q.Aggregate.FillsZero(q.Source) {
		series.FillZeros()
	}
	return series, nil
}

// bucketScanner is what fetchSQL needs from pgx.Rows, declared here where it is
// consumed so the scan can be exercised without a database.
type bucketScanner interface {
	Scan(dest ...any) error
}

func scanBucket(rows bucketScanner, series *alerting.Series, ratio bool) error {
	var bucket time.Time
	var value, denominator *float64
	dest := []any{&bucket, &value}
	if ratio {
		dest = append(dest, &denominator)
	}
	if err := rows.Scan(dest...); err != nil {
		return fmt.Errorf("source: scan a bucket: %w", err)
	}
	index, ok := series.IndexOf(bucket)
	if !ok {
		// A bucket outside the span asked for. Not an error worth failing a
		// whole evaluation over — the range is half-open and a boundary row is
		// the ordinary way this happens — but it is not written either.
		return nil
	}
	switch {
	case ratio:
		// A ratio's numerator and denominator arrive from one scan, so the two
		// can never disagree about which rows they counted. A null denominator
		// means the group had none, which stays unknown rather than becoming a
		// zero-percent nobody measured.
		if value == nil || denominator == nil {
			return nil
		}
		series.SetRatio(index, *value, *denominator)
	case value != nil:
		series.Set(index, *value)
	}
	return nil
}

// intervalOf renders a step as a Postgres interval literal. Seconds, because
// every permitted step is a whole number of them and a unit-free number is not an
// interval.
func intervalOf(step time.Duration) string {
	return fmt.Sprintf("%d seconds", int64(step.Seconds()))
}

// scopeSQL appends the scope's predicates, returning the SQL fragment and the
// grown argument list.
//
// Every value is a placeholder. The column names and the SELECT list are chosen
// from closed catalogues in this package and never from a request, which is what
// keeps a jsonb-stored watch definition from reaching the query planner as text.
func scopeSQL(src alerting.Source, scope alerting.Scope, args []any) (string, []any) {
	var b strings.Builder
	// The %d is always the placeholder just appended, so the clause and its
	// argument cannot drift apart as predicates are added or removed.
	add := func(format string, value any) {
		args = append(args, value)
		fmt.Fprintf(&b, " AND "+format, len(args))
	}
	if scope.DeploymentID != "" {
		// Cast explicitly: the column is uuid and the scope carries text, and an
		// inference that happens to work is not the same as one that is written
		// down.
		add("deployment_id = $%d::uuid", scope.DeploymentID)
	}
	if scope.AppName != "" {
		add("app_name = $%d", scope.AppName)
	}
	if scope.AppVersion != "" {
		add("app_version = $%d", scope.AppVersion)
	}
	if src == alerting.SourceTraces && scope.IntegrationID != "" {
		add("integration_id = $%d::uuid", scope.IntegrationID)
	}
	if src == alerting.SourceLogs {
		if len(scope.Levels) > 0 {
			add("lower(level) = ANY($%d)", lowered(scope.Levels))
		}
		if scope.Search != "" {
			// A substring predicate has no index behind it — idx_logs_deployment_ts_id
			// serves the deployment and the window, and this filters on top. It is
			// affordable because a watch always reads a bounded window, and the
			// watch validator requires a deployment scope alongside a search.
			add("message ILIKE $%d", "%"+scope.Search+"%")
		}
	}
	return b.String(), args
}

func lowered(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strings.ToLower(v)
	}
	return out
}

// ingestingSQL asks whether anything at all has arrived since a cutoff.
//
// Two EXISTS rather than a count or a max: the question is "is anything still
// coming", the answer is a boolean, and an EXISTS stops at the first row it finds
// on an index that is already ordered the right way. On an idle installation both
// halves scan nothing and return false immediately.
//
// received_at rather than ts, deliberately. ts is when the traced thing happened,
// which a publisher stamps and can backdate; received_at is when this service
// wrote the row, which is the only column that answers "is the pipeline alive".
//
// trace_summaries is in the list even though it is derived from traces, and the
// smoke test that put it here is the argument: it is the table most conditions
// read, and a probe that missed it would suppress every downward watch on an
// installation whose summaries were arriving perfectly well.
const ingestingSQL = `
SELECT EXISTS (SELECT 1 FROM logs WHERE received_at >= $1)
    OR EXISTS (SELECT 1 FROM traces WHERE received_at >= $1)
    OR EXISTS (SELECT 1 FROM trace_summaries WHERE last_seen_at >= $1)`

// Ingesting reports whether any telemetry has been stored since the given time.
//
// One cheap query per tick, shared by every watch, and it exists because a
// pipeline that has stopped delivering looks exactly like an installation with no
// traffic — a difference no individual condition can see, and one that every
// downward condition would otherwise fire on at once.
func (f *Fetcher) Ingesting(ctx context.Context, since time.Time) (bool, error) {
	if f.pool == nil {
		// No database means no evidence either way. Reported as "yes" so the
		// caller does not suppress every downward watch on the strength of a
		// question it could not ask.
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var ingesting bool
	if err := f.pool.QueryRow(ctx, ingestingSQL, since).Scan(&ingesting); err != nil {
		return false, fmt.Errorf("source: check whether telemetry is still arriving: %w", err)
	}
	return ingesting, nil
}
