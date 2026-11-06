package testrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
)

// runColumns is the column list (and order) scanRun expects.
const runColumns = `id, deployment_id, snapshot_id, runtime_version, status, rollout_on_pass,
	rolled_out_at, report, error, created_by, created_at, finished_at`

// Repo persists test runs to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create records a running test run; id and created_at come back.
func (r *Repo) Create(ctx context.Context, run Run) (Run, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO deployment_test_runs
		     (deployment_id, snapshot_id, runtime_version, status, rollout_on_pass, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+runColumns,
		run.DeploymentID, run.SnapshotID, run.RuntimeVersion, StatusRunning, run.RolloutOnPass, run.CreatedBy,
	)
	ret, err := scanRun(row)
	if err != nil {
		return Run{}, fmt.Errorf("test run repo: create: %w", err)
	}
	return ret, nil
}

// Get returns one of a deployment's test runs, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, deploymentID, id string) (Run, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+runColumns+` FROM deployment_test_runs WHERE id = $1 AND deployment_id = $2`,
		id, deploymentID,
	)
	ret, err := scanRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("test run repo: get: %w", err)
	}
	return ret, nil
}

// ListByDeployment returns a deployment's most recent test runs, newest first.
func (r *Repo) ListByDeployment(ctx context.Context, deploymentID string, limit int) ([]Run, error) {
	return r.list(ctx, "list by deployment",
		`SELECT `+runColumns+` FROM deployment_test_runs
		 WHERE deployment_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`,
		deploymentID, limit)
}

// Latest returns each deployment's most recent test run.
func (r *Repo) Latest(ctx context.Context) ([]Run, error) {
	return r.list(ctx, "latest",
		`SELECT DISTINCT ON (deployment_id) `+runColumns+` FROM deployment_test_runs
		 WHERE deployment_id IN (SELECT id FROM integration_deployments
		                         WHERE environment IS NOT DISTINCT FROM $1)
		 ORDER BY deployment_id, created_at DESC, id DESC`, current.Column(ctx))
}

// ListRunningSince returns the runs still running that were created before t.
func (r *Repo) ListRunningSince(ctx context.Context, t time.Time) ([]Run, error) {
	return r.list(ctx, "list running",
		`SELECT `+runColumns+` FROM deployment_test_runs
		 WHERE status = $1 AND created_at < $2
		   AND deployment_id IN (SELECT id FROM integration_deployments
		                         WHERE environment IS NOT DISTINCT FROM $3)`,
		StatusRunning, t, current.Column(ctx))
}

// Finish records a running run's verdict, and reports whether this call was the
// one that did. A run that is no longer running is left alone, which is what
// lets every replica try without two of them acting on one verdict.
func (r *Repo) Finish(ctx context.Context, id string, res Result) (bool, error) {
	var report any
	if res.Report != nil {
		report = res.Report
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE deployment_test_runs
		 SET status = $2, report = $3, error = $4, finished_at = now()
		 WHERE id = $1 AND status = $5`,
		id, res.Status, report, res.Error, StatusRunning,
	)
	if err != nil {
		return false, fmt.Errorf("test run repo: finish: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// RecordRollout records how the rollout a passing run asked for went: when it
// happened, or why it did not.
func (r *Repo) RecordRollout(ctx context.Context, id string, at *time.Time, rolloutErr string) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE deployment_test_runs SET rolled_out_at = $2, error = $3 WHERE id = $1`,
		id, at, rolloutErr,
	); err != nil {
		return fmt.Errorf("test run repo: record rollout: %w", err)
	}
	return nil
}

func (r *Repo) list(ctx context.Context, op, query string, args ...any) ([]Run, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("test run repo: %s: %w", op, err)
	}
	ret, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Run, error) {
		return scanRun(row)
	})
	if err != nil {
		return nil, fmt.Errorf("test run repo: %s: %w", op, err)
	}
	return ret, nil
}

func scanRun(row pgx.Row) (Run, error) {
	var ret Run
	var status string
	var report []byte
	if err := row.Scan(&ret.ID, &ret.DeploymentID, &ret.SnapshotID, &ret.RuntimeVersion, &status,
		&ret.RolloutOnPass, &ret.RolledOutAt, &report, &ret.Error, &ret.CreatedBy, &ret.CreatedAt,
		&ret.FinishedAt); err != nil {
		return Run{}, err //nolint:wrapcheck // callers label it, and check for pgx.ErrNoRows
	}
	ret.Status = Status(status)
	if report != nil {
		ret.Report = json.RawMessage(report)
	}
	return ret, nil
}
