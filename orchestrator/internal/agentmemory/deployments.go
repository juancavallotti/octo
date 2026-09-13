package agentmemory

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// deploymentLookup resolves a deployment to the integration it belongs to.
//
// It is a query here rather than a call into the deployment package because that
// package is only wired when there is cluster access, and one column from one row
// does not justify tying this feature's availability to that one's.
//
// The relation is immutable: a deployment is created for an integration and
// never moves, which is what makes the Service's cache of this safe.
type deploymentLookup struct {
	pool *pgxpool.Pool
}

// NewDeploymentLookup returns the resolver a Service uses.
func NewDeploymentLookup(pool *pgxpool.Pool) Deployments {
	return &deploymentLookup{pool: pool}
}

// IntegrationID returns the integration a deployment belongs to.
func (d *deploymentLookup) IntegrationID(ctx context.Context, deploymentID string) (string, error) {
	var id string
	err := d.pool.QueryRow(ctx,
		`SELECT integration_id FROM integration_deployments WHERE id = $1`, deploymentID,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A pod writing under a deployment row that is gone. Not found rather than a
			// server error: the deployment was deleted out from under a run that is still
			// finishing, which is ordinary during an undeploy.
			return "", fmt.Errorf("%w: no deployment %q", ErrNotFound, deploymentID)
		}
		return "", fmt.Errorf("agent memory: resolve deployment: %w", err)
	}
	return id, nil
}
