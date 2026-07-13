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
