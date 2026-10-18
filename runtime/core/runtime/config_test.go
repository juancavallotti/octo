package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// unnamed wraps configs that never were files.
func unnamed(configs ...types.Config) []ConfigFile {
	files := make([]ConfigFile, 0, len(configs))
	for _, cfg := range configs {
		files = append(files, ConfigFile{Config: cfg})
	}
	return files
}

func TestMergeConfigsConcatenates(t *testing.T) {
	merged, err := MergeConfigs(unnamed(
		types.Config{
			Service:    types.ServiceConfig{Name: "svc"},
			Connectors: []types.ConnectorConfig{{Name: "a", Type: "http"}},
			Flows:      []types.FlowConfig{{Name: "f1"}},
		},
		types.Config{
			Connectors: []types.ConnectorConfig{{Name: "b", Type: "cron"}},
			Processors: []types.ProcessorConfig{{Name: "p1", Type: "log"}},
			Flows:      []types.FlowConfig{{Name: "f2"}},
		},
	))
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
	merged, err := MergeConfigs(unnamed(
		types.Config{Env: []types.EnvVar{{Name: "SHARED"}, {Name: "A"}}},
		types.Config{Env: []types.EnvVar{{Name: "SHARED"}, {Name: "B"}}},
	))
	if err != nil {
		t.Fatalf("MergeConfigs: %v", err)
	}
	// SHARED declared in both files is allowed and folded to one entry.
	if len(merged.Env) != 3 {
		t.Errorf("env count = %d, want 3 (SHARED, A, B): %v", len(merged.Env), merged.Env)
	}
}

func TestMergeConfigsDedupesEnvResources(t *testing.T) {
	merged, err := MergeConfigs(unnamed(
		types.Config{Resources: types.ResourcesConfig{Env: []string{".env.dev", ".env.local"}}},
		types.Config{Resources: types.ResourcesConfig{Env: []string{".env.dev", ".env.prod"}}},
	))
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
		files   []ConfigFile
		wantErr string
	}{
		{
			name: "duplicate flow across files",
			files: []ConfigFile{
				{Path: "a.yaml", Config: types.Config{Flows: []types.FlowConfig{{Name: "dup"}}}},
				{Path: "b.yaml", Config: types.Config{Flows: []types.FlowConfig{{Name: "dup"}}}},
			},
			wantErr: `flow "dup" is defined in both a.yaml and b.yaml`,
		},
		{
			name: "duplicate connector in one file",
			files: []ConfigFile{
				{Path: "a.yaml", Config: types.Config{Connectors: []types.ConnectorConfig{{Name: "c"}, {Name: "c"}}}},
			},
			wantErr: `connector "c" is defined more than once in a.yaml`,
		},
		{
			name: "duplicate processor, no paths",
			files: unnamed(
				types.Config{Processors: []types.ProcessorConfig{{Name: "p"}}},
				types.Config{Processors: []types.ProcessorConfig{{Name: "p"}}},
			),
			wantErr: `processor "p" is defined more than once`,
		},
		{
			name: "two services",
			files: []ConfigFile{
				{Path: "a.yaml", Config: types.Config{Service: types.ServiceConfig{Name: "one"}}},
				{Path: "b.yaml", Config: types.Config{Service: types.ServiceConfig{Name: "two"}}},
			},
			wantErr: "service identity is declared in both a.yaml and b.yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeConfigs(tc.files)
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

// TestLoadConfigWithUnsetEnvStandsIn: checking a config's shape must not need its
// secrets, so an unset variable resolves to the empty string, and only the required
// one — the one a run would refuse — is reported.
func TestLoadConfigWithUnsetEnvStandsIn(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "app.yaml", `
env:
  - name: OCTO_TEST_UNSET_KEY
    required: true
  - name: OCTO_TEST_UNSET_REGION
connectors:
  - name: api
    type: http-client
    settings:
      token: ${OCTO_TEST_UNSET_KEY}
      region: ${OCTO_TEST_UNSET_REGION}
`)
	if _, err := LoadConfig(dir, core.NoopResourceLoader{}); err == nil {
		t.Fatal("LoadConfig without the option: want the unset required variable refused")
	}

	var reported []string
	cfg, err := LoadConfig(dir, core.NoopResourceLoader{}, WithUnsetEnv(func(name string) {
		reported = append(reported, name)
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(reported) != 1 || reported[0] != "OCTO_TEST_UNSET_KEY" {
		t.Errorf("reported = %v, want only the required variable", reported)
	}
	if got := cfg.Connectors[0].Settings["token"]; got != "" {
		t.Errorf("token = %#v, want the empty stand-in", got)
	}
}
