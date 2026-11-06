package user

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// Grants per environment are a unique index and a check constraint, which are
// the database's and not this package's, so they are pinned against Postgres:
// the same role in two environments, the default's roles kept apart from the
// others', admin refused outside the default, and an unknown environment
// refused by its foreign key.
func TestGrantsArePerEnvironment(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO environments (name, namespace) VALUES ('grantstest', 'octo-grantstest') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed environment: %v", err)
	}
	t.Cleanup(func() { _, _ = r.pool.Exec(context.Background(), `DELETE FROM environments WHERE name = 'grantstest'`) })

	u, err := r.Create(ctx, "envgrants@example.com", "Env Grants")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, environment := range []string{"", "grantstest", "grantstest"} {
		if err := r.GrantIn(ctx, u.ID, environment, RoleDeveloper, nil); err != nil {
			t.Fatalf("GrantIn(%q): %v", environment, err)
		}
	}
	if err := r.GrantIn(ctx, u.ID, "grantstest", RoleOperator, nil); err != nil {
		t.Fatalf("GrantIn operator: %v", err)
	}

	got, err := r.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(got.Roles, []Role{RoleDeveloper}) {
		t.Errorf("default roles = %v, want developer", got.Roles)
	}
	staging := slices.Clone(got.EnvRoles["grantstest"])
	slices.Sort(staging)
	if !slices.Equal(staging, []Role{RoleDeveloper, RoleOperator}) {
		t.Errorf("envRoles[grantstest] = %v, want developer and operator", staging)
	}

	if err := r.GrantIn(ctx, u.ID, "grantstest", RoleAdmin, nil); err == nil {
		t.Error("admin was granted outside the default")
	}
	if err := r.GrantIn(ctx, u.ID, "nowhere", RoleDeveloper, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown environment: err = %v, want ErrInvalid", err)
	}

	if err := r.RevokeIn(ctx, u.ID, "grantstest", RoleDeveloper); err != nil {
		t.Fatalf("RevokeIn: %v", err)
	}
	got, _ = r.Get(ctx, u.ID)
	if !slices.Equal(got.Roles, []Role{RoleDeveloper}) || !slices.Equal(got.EnvRoles["grantstest"], []Role{RoleOperator}) {
		t.Errorf("after revoking in grantstest: roles %v, envRoles %v", got.Roles, got.EnvRoles)
	}
}
