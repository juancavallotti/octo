package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The definition is no longer a column, so what a caller gets back is a merge of
// the integration's config files. With one file — every integration today — that
// has to come back exactly as it was written.
func TestDefinitionRoundTripsVerbatim(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	definition := "service:\n  name: orders\n\n# a comment worth keeping\nflows:\n  - name: intake\n"
	created := createIntegration(t, r, "verbatim-definition", definition)
	if created.Definition != definition {
		t.Errorf("create returned %q, want it unchanged", created.Definition)
	}

	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Definition != definition {
		t.Errorf("get returned %q, want it unchanged", got.Definition)
	}
}

// A project with several config files reports them merged, which is what lets
// the rest of the orchestrator keep speaking in one definition.
func TestDefinitionMergesEveryConfigFile(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "merged-definition", "service:\n  name: orders\nflows:\n  - name: intake\n")
	addFile(t, r, created.ID, "extra.yaml", "config", "flows:\n  - name: dispatch\n")

	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, want := range []string{"intake", "dispatch", "orders"} {
		if !strings.Contains(got.Definition, want) {
			t.Errorf("merged definition is missing %q:\n%s", want, got.Definition)
		}
	}
}

// Resources and test suites sit in the same table and must stay out of the
// definition — the runtime walks past a _test.yaml, and so do we.
func TestDefinitionIgnoresNonConfigFiles(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	definition := "flows:\n  - name: intake\n"
	created := createIntegration(t, r, "ignores-non-config", definition)
	addFile(t, r, created.ID, "intake_test.yaml", "test", "flow: intake\ncases: []\n")
	addFile(t, r, created.ID, ".env.dev", "resource", "TOKEN=abc")

	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Definition != definition {
		t.Errorf("definition picked up a non-config file:\n%s", got.Definition)
	}
}

// A whole definition has no single file it belongs to once a project has
// several, and collapsing them to answer that would destroy the others.
func TestUpdateRefusesAnAmbiguousDefinition(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "ambiguous-definition", "flows:\n  - name: intake\n")
	addFile(t, r, created.ID, "extra.yaml", "config", "flows:\n  - name: dispatch\n")

	_, err := r.Update(ctx, created.ID, "ambiguous-definition", "flows:\n  - name: replaced\n", "")
	if !errors.Is(err, ErrAmbiguousDefinition) {
		t.Fatalf("update err = %v, want ErrAmbiguousDefinition", err)
	}

	// and the refusal left both files alone
	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.Contains(got.Definition, "replaced") {
		t.Errorf("a refused write still landed:\n%s", got.Definition)
	}
	if !strings.Contains(got.Definition, "dispatch") {
		t.Errorf("a refused write lost a file:\n%s", got.Definition)
	}
}

// Updating rewrites the file the definition already lives in rather than
// appending a second one.
func TestUpdateRewritesTheSameFile(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "rewrites-same-file", "flows:\n  - name: intake\n")
	if _, err := r.Update(ctx, created.ID, "rewrites-same-file", "flows:\n  - name: changed\n", ""); err != nil {
		t.Fatalf("update: %v", err)
	}

	paths := configPaths(t, r, created.ID)
	if len(paths) != 1 || paths[0] != defaultConfigPath {
		t.Errorf("config files = %v, want just %q", paths, defaultConfigPath)
	}
}

// A definition written to an integration that has none yet creates the file
// instead of failing — the path bundle import and the agent installer take.
func TestUpdateCreatesTheConfigFileWhenThereIsNone(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	created := createIntegration(t, r, "creates-config-file", "")
	if paths := configPaths(t, r, created.ID); len(paths) != 0 {
		t.Fatalf("expected no config files, got %v", paths)
	}

	updated, err := r.Update(ctx, created.ID, "creates-config-file", "flows:\n  - name: intake\n", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(updated.Definition, "intake") {
		t.Errorf("definition = %q, want the written body", updated.Definition)
	}
}

// addFile writes a file straight to the table, standing in for the paths that
// will put more than one config file on an integration.
func addFile(t *testing.T, r *Repo, integrationID, path, role, content string) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(),
		`INSERT INTO integration_files (integration_id, path, role, kind, content)
		 VALUES ($1, $2, $3, '', $4)`,
		integrationID, path, role, content)
	if err != nil {
		t.Fatalf("add file %s: %v", path, err)
	}
}

// configPaths lists an integration's config files, ordered.
func configPaths(t *testing.T, r *Repo, integrationID string) []string {
	t.Helper()
	rows, err := r.pool.Query(context.Background(),
		`SELECT path FROM integration_files
		  WHERE integration_id = $1 AND role = 'config' ORDER BY path`, integrationID)
	if err != nil {
		t.Fatalf("list config files: %v", err)
	}
	defer rows.Close()

	var ret []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan path: %v", err)
		}
		ret = append(ret, p)
	}
	return ret
}
