package integration

import (
	"context"
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
