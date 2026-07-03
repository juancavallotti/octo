package runtime

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// fakeEnvLoader serves env resources from an in-memory map; an id absent from the
// map (or explicitly nil bytes for "unreadable") drives the not-found path.
type fakeEnvLoader map[string][]byte

func (f fakeEnvLoader) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	data, ok := f[id]
	if !ok {
		return nil, core.ErrResourceNotFound
	}
	return data, nil
}

func TestApplyEnvCombinesResources(t *testing.T) {
	cfg := types.Config{
		Env:       []types.EnvVar{{Name: "TOKEN", Required: true}},
		Resources: types.ResourcesConfig{Env: []string{".env.dev"}},
	}
	loader := fakeEnvLoader{".env.dev": []byte("TOKEN=abc")}
	if err := applyEnv(&cfg, loader); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}
	if got := cfg.ResolvedEnv["TOKEN"]; got != "abc" {
		t.Errorf("TOKEN = %q, want abc (from env resource)", got)
	}
}

func TestApplyEnvResourceLoserToOSEnv(t *testing.T) {
	t.Setenv("TOKEN", "from-os")
	cfg := types.Config{
		Env:       []types.EnvVar{{Name: "TOKEN", Required: true}},
		Resources: types.ResourcesConfig{Env: []string{".env.dev"}},
	}
	loader := fakeEnvLoader{".env.dev": []byte("TOKEN=from-resource")}
	if err := applyEnv(&cfg, loader); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}
	if got := cfg.ResolvedEnv["TOKEN"]; got != "from-os" {
		t.Errorf("TOKEN = %q, want from-os (OS env beats env resource)", got)
	}
}

func TestApplyEnvMissingResourceSkipped(t *testing.T) {
	t.Setenv("TOKEN", "from-os")
	cfg := types.Config{
		Env:       []types.EnvVar{{Name: "TOKEN", Required: true}},
		Resources: types.ResourcesConfig{Env: []string{".env.dev"}},
	}
	// The declared resource does not exist: it is skipped, not an error, and TOKEN
	// still resolves from the OS environment.
	if err := applyEnv(&cfg, fakeEnvLoader{}); err != nil {
		t.Fatalf("applyEnv with missing resource: %v", err)
	}
	if got := cfg.ResolvedEnv["TOKEN"]; got != "from-os" {
		t.Errorf("TOKEN = %q, want from-os", got)
	}
}

func TestApplyEnvRequiredMissingStillCrashes(t *testing.T) {
	cfg := types.Config{
		Env:       []types.EnvVar{{Name: "TOKEN", Required: true}},
		Resources: types.ResourcesConfig{Env: []string{".env.dev"}},
	}
	// The resource is missing and TOKEN is supplied nowhere else: the required
	// variable is genuinely unset, so applyEnv must fail.
	if err := applyEnv(&cfg, fakeEnvLoader{}); err == nil {
		t.Fatal("expected applyEnv to fail for an unset required variable")
	}
}

func TestApplyEnvUnparseableResourceFallsBack(t *testing.T) {
	t.Setenv("TOKEN", "from-os")
	cfg := types.Config{
		Env:       []types.EnvVar{{Name: "TOKEN", Required: true}},
		Resources: types.ResourcesConfig{Env: []string{".env.dev"}},
	}
	// A malformed env resource is warned and skipped (not fatal); TOKEN falls back
	// to the OS environment.
	loader := fakeEnvLoader{".env.dev": []byte("this is : not = valid env\n=bad")}
	if err := applyEnv(&cfg, loader); err != nil {
		t.Fatalf("applyEnv with unparseable resource: %v", err)
	}
	if got := cfg.ResolvedEnv["TOKEN"]; got != "from-os" {
		t.Errorf("TOKEN = %q, want from-os", got)
	}
}
