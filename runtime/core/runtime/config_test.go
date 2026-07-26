package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestMergeConfigsConcatenates(t *testing.T) {
	merged, err := MergeConfigs([]types.Config{
		{
			Service:    types.ServiceConfig{Name: "svc"},
			Connectors: []types.ConnectorConfig{{Name: "a", Type: "http"}},
			Flows:      []types.FlowConfig{{Name: "f1"}},
		},
		{
			Connectors: []types.ConnectorConfig{{Name: "b", Type: "cron"}},
			Processors: []types.ProcessorConfig{{Name: "p1", Type: "log"}},
			Flows:      []types.FlowConfig{{Name: "f2"}},
		},
	})
	if err != nil {
		t.Fatalf("MergeConfigs: %v", err)
	}
	if merged.Service.Name != "svc" {
		t.Errorf("service name = %q, want svc", merged.Service.Name)
	}
	if len(merged.Connectors) != 2 || len(merged.Flows) != 2 || len(merged.Processors) != 1 {
		t.Errorf("unexpected merge: connectors=%d flows=%d processors=%d",
			len(merged.Connectors), len(merged.Flows), len(merged.Processors))
	}
}

func TestMergeConfigsDedupesEnv(t *testing.T) {
	merged, err := MergeConfigs([]types.Config{
		{Env: []types.EnvVar{{Name: "SHARED"}, {Name: "A"}}},
		{Env: []types.EnvVar{{Name: "SHARED"}, {Name: "B"}}},
	})
	if err != nil {
		t.Fatalf("MergeConfigs: %v", err)
	}
	// SHARED declared in both files is allowed and folded to one entry.
	if len(merged.Env) != 3 {
		t.Errorf("env count = %d, want 3 (SHARED, A, B): %v", len(merged.Env), merged.Env)
	}
}

func TestMergeConfigsDedupesEnvResources(t *testing.T) {
	merged, err := MergeConfigs([]types.Config{
		{Resources: types.ResourcesConfig{Env: []string{".env.dev", ".env.local"}}},
		{Resources: types.ResourcesConfig{Env: []string{".env.dev", ".env.prod"}}},
	})
	if err != nil {
		t.Fatalf("MergeConfigs: %v", err)
	}
	// The same id in both files folds to one entry, first-seen order preserved.
	want := []string{".env.dev", ".env.local", ".env.prod"}
	if got := merged.Resources.Env; len(got) != len(want) {
		t.Fatalf("env resources = %v, want %v", got, want)
	}
	for i, id := range want {
		if merged.Resources.Env[i] != id {
			t.Errorf("env resource[%d] = %q, want %q", i, merged.Resources.Env[i], id)
		}
	}
}

func TestParseConfigDecodesResources(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
service:
  name: demo
resources:
  env:
    - .env.dev
    - .env.local
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	want := []string{".env.dev", ".env.local"}
	if got := cfg.Resources.Env; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("resources.env = %v, want %v", got, want)
	}
}

func TestMergeConfigsRejectsDuplicates(t *testing.T) {
	tests := []struct {
		name    string
		configs []types.Config
		wantErr string
	}{
		{
			name: "duplicate flow",
			configs: []types.Config{
				{Flows: []types.FlowConfig{{Name: "dup"}}},
				{Flows: []types.FlowConfig{{Name: "dup"}}},
			},
			wantErr: "flow \"dup\"",
		},
		{
			name: "duplicate connector",
			configs: []types.Config{
				{Connectors: []types.ConnectorConfig{{Name: "c"}}},
				{Connectors: []types.ConnectorConfig{{Name: "c"}}},
			},
			wantErr: "connector \"c\"",
		},
		{
			name: "two services",
			configs: []types.Config{
				{Service: types.ServiceConfig{Name: "one"}},
				{Service: types.ServiceConfig{Name: "two"}},
			},
			wantErr: "service identity",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeConfigs(tc.configs)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestLoadConfigDirResolvesAcrossFiles pins the ordering the whole load hangs on.
// Substitution has to run before the documents are typed, but declarations are
// still gathered across every file first — so a variable declared in one file
// serves a reference in another, exactly as it did when the merge came first.
func TestLoadConfigDirResolvesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "a-env.yaml", `
env:
  - name: FLOW_WORKERS
    default: "64"
`)
	writeConfig(t, dir, "b-flow.yaml", `
flows:
  - name: orders
    workers: ${FLOW_WORKERS}
    process:
      - type: log
`)

	cfg, err := LoadConfig(dir, core.NoopResourceLoader{})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Flows[0].Workers; got != 64 {
		t.Errorf("workers = %d, want 64 (declared in a sibling file)", got)
	}
	if got := cfg.ResolvedEnv["FLOW_WORKERS"]; got != "64" {
		t.Errorf("ResolvedEnv[FLOW_WORKERS] = %q, want 64; the merge does not carry it", got)
	}
}

// TestLoadConfigDirSkipsEmptyFile: an empty document has nothing to decode, and
// that is not an error — the same as it was when the file went straight into the
// decoder.
func TestLoadConfigDirSkipsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "empty.yaml", "\n  \n")
	writeConfig(t, dir, "flow.yaml", `
flows:
  - name: orders
    process:
      - type: log
`)

	cfg, err := LoadConfig(dir, core.NoopResourceLoader{})
	if err != nil {
		t.Fatalf("LoadConfig with an empty file: %v", err)
	}
	if len(cfg.Flows) != 1 {
		t.Errorf("flows = %d, want 1", len(cfg.Flows))
	}
}

func writeConfig(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
