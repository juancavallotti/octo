package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deployments answers which integration owns a deployment.
//
// Trace records arrive carrying only the deployment that emitted them, because
// that is all the emitting pod is told. Attributing them to an integration is
// what makes "what has this integration cost" answerable without joining every
// query back to the deployment tables.
type Deployments struct {
	pool *pgxpool.Pool
}

// NewDeployments returns a lookup backed by pool.
func NewDeployments(pool *pgxpool.Pool) *Deployments {
	return &Deployments{pool: pool}
}

// IntegrationOf returns the integration owning a deployment, reporting false when
// no such deployment exists.
//
// A deployment never changes hands, so the answer is immutable and a caller may
// cache a hit indefinitely. An unknown id is a distinct outcome from an error:
// records outlive the deployments that produced them, and one arriving for a
// deployment that has since been deleted is expected rather than exceptional.
func (d *Deployments) IntegrationOf(ctx context.Context, deploymentID string) (string, bool, error) {
	var integrationID string
	err := d.pool.QueryRow(ctx,
		`SELECT integration_id FROM integration_deployments WHERE id = $1::uuid`,
		deploymentID).Scan(&integrationID)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("repo: resolve deployment %q: %w", deploymentID, err)
	default:
		return integrationID, true, nil
	}
}
