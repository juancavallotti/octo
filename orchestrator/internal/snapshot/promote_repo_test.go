package snapshot

import (
	"context"
	"errors"
	"testing"
)

// The family link, the per-environment uniqueness and the copy are all the
// database's, so promotion is pinned against Postgres: the first promotion
// creates the target with a working copy, the next lands on the same one, env
// files stay the target's own, and a tag already there is refused.
func TestPromoteAgainstPostgres(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewRepo(pool)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO environments (name, namespace) VALUES ('promotest', 'octo-promotest') ON CONFLICT DO NOTHING`)
	var source string
	if err := pool.QueryRow(ctx, `INSERT INTO integrations (name) VALUES ('promote-orders') RETURNING id`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM integrations WHERE promoted_from = $1`, source)
		_, _ = pool.Exec(c, `DELETE FROM integrations WHERE id = $1`, source)
		_, _ = pool.Exec(c, `DELETE FROM environments WHERE name = 'promotest'`)
	})
	exec(`INSERT INTO integration_files (integration_id, kind, path, content, role) VALUES
	      ($1, 'flow', 'flows.yaml', 'flows: [v1]', 'config'),
	      ($1, 'env', '.env', 'HOST=prod', 'resource')`, source)
	v1, err := repo.Create(ctx, source, "v1")
	if err != nil {
		t.Fatal(err)
	}

	first, err := repo.Promote(ctx, v1.ID, "promotest", "")
	if err != nil {
		t.Fatalf("first promotion: %v", err)
	}
	if !first.Created {
		t.Error("the first promotion did not create the target")
	}
	var working int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM integration_files WHERE integration_id = $1 AND kind <> 'env'`,
		first.IntegrationID).Scan(&working)
	if working != 1 {
		t.Errorf("the new target's working copy has %d files, want the flow", working)
	}
	var envFrozen int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM integration_file_snapshots WHERE snapshot_id = $1 AND kind = 'env'`,
		first.Snapshot.ID).Scan(&envFrozen)
	if envFrozen != 0 {
		t.Error("the source's env file travelled with the version")
	}

	// The target gets an env file of its own, and the source a second version.
	exec(`INSERT INTO integration_files (integration_id, kind, path, content, role)
	      VALUES ($1, 'env', '.env', 'HOST=staging', 'resource')`, first.IntegrationID)
	exec(`UPDATE integration_files SET content = 'flows: [v2]' WHERE integration_id = $1 AND kind = 'flow'`, source)
	v2, err := repo.Create(ctx, source, "v2")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Promote(ctx, v2.ID, "promotest", "")
	if err != nil {
		t.Fatalf("second promotion: %v", err)
	}
	if second.Created || second.IntegrationID != first.IntegrationID {
		t.Errorf("the second promotion landed on %s (created %v), want %s", second.IntegrationID, second.Created, first.IntegrationID)
	}
	var env string
	_ = pool.QueryRow(ctx, `SELECT content FROM integration_file_snapshots WHERE snapshot_id = $1 AND kind = 'env'`,
		second.Snapshot.ID).Scan(&env)
	if env != "HOST=staging" {
		t.Errorf("the promoted version's env file = %q, want the target's own", env)
	}

	if _, err := repo.Promote(ctx, v2.ID, "promotest", ""); !errors.Is(err, ErrTagExists) {
		t.Errorf("promoting the same version twice: err = %v, want ErrTagExists", err)
	}
}
