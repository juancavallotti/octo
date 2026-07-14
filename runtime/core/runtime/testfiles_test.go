package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// writeFile writes content into dir under name.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// ordersYAML is a config directory's real content: one flow.
const ordersYAML = `
flows:
  - name: orders
    process:
      - type: log
        name: say-it
        settings:
          message: '"hi"'
`

// ordersTestYAML is what a dolphin suite for it looks like. It deliberately also
// declares a flow named `orders` — so if loadDir ever stops skipping test files,
// MergeConfigs rejects the duplicate name and this test fails loudly.
//
// Asserting on the duplicate rather than on the dolphin keys is the point: a test
// file parses into an EMPTY config today, because the YAML decoder ignores unknown
// keys, so "it merged in and contributed nothing" and "it was skipped" look
// identical from the outside. The collision tells them apart.
const ordersTestYAML = `
flow: orders
cases:
  - name: it greets
    expect:
      body: { ok: true }

flows:
  - name: orders
    process: []
`

// TestLoadConfigSkipsTestFiles: a suite lives beside the flows it exercises — that is
// the whole convention — so loading the directory must walk past it, the way the Go
// compiler walks past a _test.go.
func TestLoadConfigSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "orders.yaml", ordersYAML)
	writeFile(t, dir, "orders_test.yaml", ordersTestYAML)
	writeFile(t, dir, "legacy_test.yml", ordersTestYAML) // .yml counts too

	cfg, err := LoadConfig(dir, core.NoopResourceLoader{})
	if err != nil {
		t.Fatalf("a test file beside the flows broke the config load: %v", err)
	}
	if len(cfg.Flows) != 1 {
		t.Fatalf("loaded %d flows, want 1 — the test file was merged in as config", len(cfg.Flows))
	}
	if cfg.Flows[0].Name != "orders" {
		t.Errorf("flow = %q, want the one from orders.yaml", cfg.Flows[0].Name)
	}
}

// TestLoadConfigDirOfOnlyTestFilesIsEmpty: a directory holding nothing but suites has
// no config in it, and says so, rather than reporting an empty config that would start
// a runtime with no flows.
func TestLoadConfigDirOfOnlyTestFilesIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "orders_test.yaml", ordersTestYAML)

	_, err := LoadConfig(dir, core.NoopResourceLoader{})
	if err == nil {
		t.Fatal("a directory of only test files must report that it holds no config")
	}
}

// TestIsTestFile is the shared definition of the convention: dolphin discovers exactly
// the files the loader here skips, so the two must never disagree about which is which.
func TestIsTestFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "orders_test.yaml", want: true},
		{name: "orders_test.yml", want: true},
		{name: "orders.yaml", want: false},
		{name: "orders.yml", want: false},
		// "test" is not a suffix of the base name here, it is the whole of it — and a
		// file actually named _test.yaml is a suite with no flow file, which is odd but
		// not a config.
		{name: "_test.yaml", want: true},
		// A flow file may perfectly well have "test" in its name without being a suite.
		{name: "test.yaml", want: false},
		{name: "testing.yaml", want: false},
		{name: "load_tests.yaml", want: false},
		// The suffix is on the base name, not the path: a directory called foo_test does
		// not make its configs into suites.
		{name: "orders_test.yaml.bak", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTestFile(tc.name); got != tc.want {
				t.Errorf("IsTestFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
