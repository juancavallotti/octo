package repo

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestDeployments opens a pool against TEST_DATABASE_URL, skipping when it is
// unset so `go test ./...` stays green without a database — the same shape the
// orchestrator's repo tests use.
func newTestDeployments(t *testing.T) (*Deployments, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run deployment lookup tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewDeployments(pool), pool
}

// seedDeployment creates an integration and a deployment of it, returning both
// ids and removing them afterwards. The integration name is unique per test so
// concurrent runs never collide on the case-insensitive name index.
func seedDeployment(t *testing.T, pool *pgxpool.Pool) (deploymentID, integrationID string) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx,
		`INSERT INTO integrations (name) VALUES ($1) RETURNING id`,
		"trace-test-"+t.Name()).Scan(&integrationID); err != nil {
		t.Fatalf("seed integration: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO integration_deployments (integration_id) VALUES ($1::uuid) RETURNING id`,
		integrationID).Scan(&deploymentID); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	t.Cleanup(func() {
		// The deployment cascades off the integration.
		_, _ = pool.Exec(context.Background(), `DELETE FROM integrations WHERE id = $1::uuid`, integrationID)
	})
	return deploymentID, integrationID
}

func TestIntegrationOfResolvesADeployment(t *testing.T) {
	lookup, pool := newTestDeployments(t)
	deploymentID, integrationID := seedDeployment(t, pool)

	got, found, err := lookup.IntegrationOf(context.Background(), deploymentID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !found {
		t.Fatal("a live deployment did not resolve")
	}
	if got != integrationID {
		t.Fatalf("integration = %q, want %q", got, integrationID)
	}
}

// Records outlive the deployments that produced them, so an id with no row is an
// expected outcome and must be reported as "no owner" rather than as a failure —
// the caller treats the two completely differently.
func TestIntegrationOfReportsAnUnknownDeployment(t *testing.T) {
	lookup, _ := newTestDeployments(t)

	got, found, err := lookup.IntegrationOf(context.Background(), "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("an unknown deployment reported an error: %v", err)
	}
	if found || got != "" {
		t.Fatalf("an unknown deployment resolved to %q", got)
	}
}

// A malformed id is a failure, not a miss: it means whatever produced it is
// broken, and silently reporting "no owner" would hide that.
func TestIntegrationOfRejectsAMalformedID(t *testing.T) {
	lookup, _ := newTestDeployments(t)

	if _, found, err := lookup.IntegrationOf(context.Background(), "not-a-uuid"); err == nil {
		t.Fatalf("a malformed id reported found=%v with no error", found)
	}
}
