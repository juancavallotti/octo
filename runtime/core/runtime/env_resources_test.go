package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
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

// unsetEnv makes each key genuinely absent for the duration of the test,
// restoring whatever the ambient environment had afterwards. Tests that assert a
// value comes from a declared default or an env resource have to say so: the OS
// environment outranks both, so a machine that happens to export TOKEN or PORT
// would otherwise quietly change what the test means. t.Setenv registers the
// restore; os.Unsetenv then removes the variable, which setting it to "" would
// not — an empty value is still a value.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

// tokenDecls is the declaration set every test here resolves: one required
// variable, supplied by a declared env resource.
func tokenDecls() envDecls {
	return envDecls{
		Env:       []types.EnvVar{{Name: "TOKEN", Required: true}},
		Resources: types.ResourcesConfig{Env: []string{".env.dev"}},
	}
}

func TestResolveDeclaredCombinesResources(t *testing.T) {
	unsetEnv(t, "TOKEN")
	loader := fakeEnvLoader{".env.dev": []byte("TOKEN=abc")}
	resolved, _, err := resolveDeclared(tokenDecls(), loader)
	if err != nil {
		t.Fatalf("resolveDeclared: %v", err)
	}
	if got := resolved["TOKEN"]; got != "abc" {
		t.Errorf("TOKEN = %q, want abc (from env resource)", got)
	}
}

func TestResolveDeclaredResourceLosesToOSEnv(t *testing.T) {
	t.Setenv("TOKEN", "from-os")
	loader := fakeEnvLoader{".env.dev": []byte("TOKEN=from-resource")}
	resolved, _, err := resolveDeclared(tokenDecls(), loader)
	if err != nil {
		t.Fatalf("resolveDeclared: %v", err)
	}
	if got := resolved["TOKEN"]; got != "from-os" {
		t.Errorf("TOKEN = %q, want from-os (OS env beats env resource)", got)
	}
}

func TestResolveDeclaredMissingResourceSkipped(t *testing.T) {
	t.Setenv("TOKEN", "from-os")
	// The declared resource does not exist: it is skipped, not an error, and TOKEN
	// still resolves from the OS environment.
	resolved, _, err := resolveDeclared(tokenDecls(), fakeEnvLoader{})
	if err != nil {
		t.Fatalf("resolveDeclared with missing resource: %v", err)
	}
	if got := resolved["TOKEN"]; got != "from-os" {
		t.Errorf("TOKEN = %q, want from-os", got)
	}
}

func TestResolveDeclaredRequiredMissingStillCrashes(t *testing.T) {
	unsetEnv(t, "TOKEN")
	// The resource is missing and TOKEN is supplied nowhere else: the required
	// variable is genuinely unset, so resolveDeclared must fail.
	if _, _, err := resolveDeclared(tokenDecls(), fakeEnvLoader{}); err == nil {
		t.Fatal("expected resolveDeclared to fail for an unset required variable")
	}
}

func TestResolveDeclaredUnparseableResourceFallsBack(t *testing.T) {
	t.Setenv("TOKEN", "from-os")
	// A malformed env resource is warned and skipped (not fatal); TOKEN falls back
	// to the OS environment.
	loader := fakeEnvLoader{".env.dev": []byte("this is : not = valid env\n=bad")}
	resolved, _, err := resolveDeclared(tokenDecls(), loader)
	if err != nil {
		t.Fatalf("resolveDeclared with unparseable resource: %v", err)
	}
	if got := resolved["TOKEN"]; got != "from-os" {
		t.Errorf("TOKEN = %q, want from-os", got)
	}
}
