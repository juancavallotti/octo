package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// fakeResourceWatcher is a ResourceLoader that also implements ResourceWatcher,
// capturing the registered callback so a test can fire a synthetic change.
type fakeResourceWatcher struct {
	fn core.ChangeFunc
}

func (fakeResourceWatcher) Load(context.Context, core.ResourceKind, string) ([]byte, error) {
	return nil, core.ErrResourceNotFound
}

//nolint:unparam // implements core.ResourceWatcher; the fake never fails but the interface returns an error
func (f *fakeResourceWatcher) OnChange(_ context.Context, fn core.ChangeFunc) error {
	f.fn = fn
	return nil
}

func TestWatchConfigForwardsResourceChanges(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("service:\n  name: demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := &fakeResourceWatcher{}
	changed, err := watchConfig(ctx, cfgPath, watcher)
	if err != nil {
		t.Fatalf("watchConfig: %v", err)
	}
	if watcher.fn == nil {
		t.Fatal("watchConfig did not register a resource change callback")
	}

	// Fire a synthetic resource change; it should surface on the debounced channel.
	watcher.fn(core.ResourceKindEnv, ".env.dev")

	select {
	case <-changed:
	case <-time.After(3 * time.Second):
		t.Fatal("resource change did not trigger a reload signal")
	}
}

// expectSignal reports whether a change arrives on changed within twice the
// debounce, which is as long as a real change takes to surface.
func expectSignal(changed <-chan struct{}) bool {
	select {
	case <-changed:
		return true
	case <-time.After(4 * watchDebounce):
		return false
	}
}

// A runtime writes into its working directory — a redirected log, the default
// trace file — and that directory is watched for ./.env. Those writes must not
// count as a config change, or every reload's own output triggers the next one.
func TestWatchConfigIgnoresUnrelatedWritesBesideTheConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte("service:\n  name: demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed, err := watchConfig(ctx, cfgPath, &fakeResourceWatcher{})
	if err != nil {
		t.Fatalf("watchConfig: %v", err)
	}

	for _, name := range []string{"run.log", "octo-trace.jsonl", "other.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("noise\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if expectSignal(changed) {
		t.Fatal("a write to a file the config does not depend on triggered a reload")
	}

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !expectSignal(changed) {
		t.Fatal("writing ./.env did not trigger a reload")
	}

	// An editor's atomic save: a temporary file renamed over the config.
	tmp := filepath.Join(dir, ".app.yaml.swp")
	if err := os.WriteFile(tmp, []byte("service:\n  name: renamed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, cfgPath); err != nil {
		t.Fatal(err)
	}
	if !expectSignal(changed) {
		t.Fatal("an atomic save of the config did not trigger a reload")
	}
}

// A config directory reloads on the files the loader reads from it, and nothing
// else: a test suite beside the flows or a stray log is not config.
func TestWatchConfigDirectoryCoversOnlyConfigFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte("flows: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed, err := watchConfig(ctx, dir, &fakeResourceWatcher{})
	if err != nil {
		t.Fatalf("watchConfig: %v", err)
	}

	for _, name := range []string{"orders_test.yaml", "run.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x: 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if expectSignal(changed) {
		t.Fatal("a file the loader skips triggered a reload")
	}

	if err := os.WriteFile(filepath.Join(dir, "billing.yml"), []byte("flows: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !expectSignal(changed) {
		t.Fatal("adding a config file to the directory did not trigger a reload")
	}
}
