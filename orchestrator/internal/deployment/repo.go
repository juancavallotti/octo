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

// Create inserts a new deployment row for integrationID with its settings, an
// initial status and metadata; id and last_updated come back via RETURNING.
func (r *Repo) Create(ctx context.Context, integrationID, status string, settings, metadata json.RawMessage) (Deployment, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO integration_deployments (integration_id, settings, status, deployment_metadata)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+deploymentColumns,
		integrationID, settings, status, metadata,
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

// IntegrationIDBySlug returns the integration id of any deployment whose metadata
// slug matches, and whether one was found. Used to keep the internal Service name
// (octo-int-{slug}) unique across integrations. If several match (the same
// integration redeployed), any one is returned — they share an integration id.
func (r *Repo) IntegrationIDBySlug(ctx context.Context, slug string) (string, bool, error) {
	return r.integrationIDByMetaField(ctx, "deployment_metadata", "slug", slug)
}

// IntegrationIDBySubdomain returns the integration id of any deployment using the
// given external subdomain, and whether one was found. Used to keep external
// hosts unique across integrations.
func (r *Repo) IntegrationIDBySubdomain(ctx context.Context, subdomain string) (string, bool, error) {
	return r.integrationIDByMetaField(ctx, "settings", "subdomain", subdomain)
}

// integrationIDByMetaField looks up the integration id of any deployment whose
// jsonb column->>field equals value. column and field are package-internal
// literals (never user input), so interpolating them into the query is safe.
func (r *Repo) integrationIDByMetaField(ctx context.Context, column, field, value string) (string, bool, error) {
	if value == "" {
		return "", false, nil
	}
	var integrationID string
	err := r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT integration_id FROM integration_deployments WHERE %s->>'%s' = $1 LIMIT 1`, column, field),
		value,
	).Scan(&integrationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("deployment repo: lookup by %s: %w", field, err)
	}
	return integrationID, true, nil
}

// SecretReferenced reports whether any deployment's settings bind an env var to
// the cluster secret name. Used to refuse deleting a secret that a live workload
// still references. The settings jsonb shape is {"env": {"VAR": {"secret": name}}};
// the jsonpath iterates env entries and matches the secret reference.
func (r *Repo) SecretReferenced(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM integration_deployments
			WHERE jsonb_path_exists(settings, '$.env.* ? (@.secret == $name)', jsonb_build_object('name', $1::text))
		)`,
		name,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("deployment repo: secret referenced: %w", err)
	}
	return exists, nil
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

// UpdateSettings replaces the settings jsonb and stamps last_updated. Returns
// ErrNotFound if id does not exist.
func (r *Repo) UpdateSettings(ctx context.Context, id string, settings json.RawMessage) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE integration_deployments SET settings = $2, last_updated = now() WHERE id = $1`,
		id, settings,
	)
	if err != nil {
		return fmt.Errorf("deployment repo: update settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMetadata replaces the deployment_metadata jsonb and stamps last_updated.
// Returns ErrNotFound if id does not exist. Used by a rollout to record the new
// version tag.
func (r *Repo) UpdateMetadata(ctx context.Context, id string, metadata json.RawMessage) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE integration_deployments SET deployment_metadata = $2, last_updated = now() WHERE id = $1`,
		id, metadata,
	)
	if err != nil {
		return fmt.Errorf("deployment repo: update metadata: %w", err)
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
