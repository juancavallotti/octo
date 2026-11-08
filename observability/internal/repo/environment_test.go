package repo

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/environment/current"
	"github.com/juancavallotti/octo/observability/internal/ingest"
)

// A log learns its environment from the deployment that emitted it, which is
// the schema's trigger and not this package's code, and is read back only in
// that environment. Pinned against Postgres for that reason.
func TestLogsAreReadOnlyInTheirEnvironment(t *testing.T) {
	pool := newPrunePool(t)
	ctx := context.Background()
	var deploymentID string
	err := pool.QueryRow(ctx, `
		WITH e AS (INSERT INTO environments (name, namespace) VALUES ('obstest', 'octo-obstest')
		           ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING name),
		     i AS (INSERT INTO integrations (name, environment) SELECT 'obstest logs', name FROM e RETURNING id, environment)
		INSERT INTO integration_deployments (integration_id, settings, status, deployment_metadata, environment)
		SELECT id, '{}', 'running', '{}', environment FROM i RETURNING id::text`).Scan(&deploymentID)
	if err != nil {
		t.Fatalf("seed a staging deployment: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM logs WHERE deployment_id = $1::uuid`, deploymentID)
		_, _ = pool.Exec(c, `DELETE FROM integration_deployments WHERE id = $1::uuid`, deploymentID)
		_, _ = pool.Exec(c, `DELETE FROM integrations WHERE name = 'obstest logs'`)
		_, _ = pool.Exec(c, `DELETE FROM environments WHERE name = 'obstest'`)
	})

	store := NewLogs(pool)
	if err := store.Insert(ctx, ingest.LogEvent{
		DeploymentID: deploymentID, Time: time.Now(), Level: "INFO", Message: "from obstest",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	count := func(c context.Context) int {
		rows, err := store.Query(c, LogFilter{DeploymentID: deploymentID, Limit: 10})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		return len(rows)
	}
	if n := count(current.With(ctx, "obstest", true)); n != 1 {
		t.Errorf("in its own environment: %d rows, want 1", n)
	}
	if n := count(ctx); n != 0 {
		t.Errorf("in the default environment: %d rows, want 0", n)
	}
}
