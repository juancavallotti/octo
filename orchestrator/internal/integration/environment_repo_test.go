package integration

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
)

// Names are unique per environment, which is the database's index and not the
// service's check, so it is pinned against Postgres: the same name may exist in
// two environments, and each environment lists only its own.
func TestNamesAndListingsArePerEnvironment(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO environments (name, namespace) VALUES ('repotest', 'octo-repotest') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed environment: %v", err)
	}
	t.Cleanup(func() { _, _ = r.pool.Exec(context.Background(), `DELETE FROM environments WHERE name = 'repotest'`) })
	staging := current.With(ctx, "repotest", true)

	prod := createIntegration(t, r, "Env Scoped Orders", "")
	other, err := r.Create(staging, "Env Scoped Orders", "", "")
	if err != nil {
		t.Fatalf("the same name in another environment: %v", err)
	}
	t.Cleanup(func() { _ = r.Delete(context.Background(), other.ID) })

	if taken, err := r.NameExists(staging, "env scoped orders", ""); err != nil || !taken {
		t.Errorf("NameExists in its own environment = %v, %v; want true", taken, err)
	}
	if taken, _ := r.NameExists(current.With(ctx, "nowhere", true), "env scoped orders", ""); taken {
		t.Error("NameExists sees another environment's name")
	}

	for name, c := range map[string]context.Context{"default": ctx, "repotest": staging} {
		items, err := r.List(c)
		if err != nil {
			t.Fatalf("List(%s): %v", name, err)
		}
		for _, it := range items {
			if (name == "default" && it.ID == other.ID) || (name == "repotest" && it.ID == prod.ID) {
				t.Errorf("List(%s) includes another environment's integration %s", name, it.ID)
			}
		}
	}
}
