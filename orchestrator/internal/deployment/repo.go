package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// deploymentColumns is the canonical column list (and order) that scanDeployment
// expects, kept in one place so reads and RETURNING clauses stay in sync.
const deploymentColumns = "id, integration_id, settings, status, deployment_metadata, last_updated"

// Repo persists deployments to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new deployment row for integrationID with an initial status
// and metadata; id, settings (default) and last_updated come back via RETURNING.
func (r *Repo) Create(ctx context.Context, integrationID, status string, metadata json.RawMessage) (Deployment, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO integration_deployments (integration_id, status, deployment_metadata)
		 VALUES ($1, $2, $3)
		 RETURNING `+deploymentColumns,
		integrationID, status, metadata,
	)
	d, err := scanDeployment(row)
	if err != nil {
		return Deployment{}, fmt.Errorf("deployment repo: create: %w", err)
	}
	return d, nil
}

// Get returns the deployment by id, or ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id string) (Deployment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+deploymentColumns+` FROM integration_deployments WHERE id = $1`, id,
	)
	d, err := scanDeployment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, fmt.Errorf("deployment repo: get: %w", err)
	}
	return d, nil
}

// ListByIntegration returns all deployments of one integration, newest first.
func (r *Repo) ListByIntegration(ctx context.Context, integrationID string) ([]Deployment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+deploymentColumns+`
		 FROM integration_deployments
		 WHERE integration_id = $1
		 ORDER BY last_updated DESC`,
		integrationID,
	)
	if err != nil {
		return nil, fmt.Errorf("deployment repo: list by integration: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Deployment, error) {
		return scanDeployment(row)
	})
	if err != nil {
		return nil, fmt.Errorf("deployment repo: list by integration: %w", err)
	}
	return items, nil
}

// UpdateStatus stamps a new cached status and last_updated. It is a no-op match
// returning ErrNotFound if id does not exist.
func (r *Repo) UpdateStatus(ctx context.Context, id, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE integration_deployments SET status = $2, last_updated = now() WHERE id = $1`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("deployment repo: update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the deployment. Returns ErrNotFound if no row was deleted.
func (r *Repo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM integration_deployments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deployment repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanDeployment reads one row in deploymentColumns order.
func scanDeployment(row pgx.Row) (Deployment, error) {
	var d Deployment
	if err := row.Scan(&d.ID, &d.IntegrationID, &d.Settings, &d.Status, &d.Metadata, &d.LastUpdated); err != nil {
		return Deployment{}, err
	}
	return d, nil
}
