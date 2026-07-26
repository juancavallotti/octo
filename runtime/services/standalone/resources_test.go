package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

func TestResourceLoaderLoadExisting(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env.dev"), "A=1")

	loader := newResourceLoader(root)
	data, err := loader.Load(context.Background(), core.ResourceKindEnv, ".env.dev")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "A=1" {
		t.Fatalf("Load = %q, want %q", data, "A=1")
	}
}

func TestResourceLoaderLoadSubfolder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "templates", "welcome.tmpl"), "hi")

	loader := newResourceLoader(root)
	data, err := loader.Load(context.Background(), core.ResourceKindTemplate, "templates/welcome.tmpl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("Load = %q, want %q", data, "hi")
	}
}

// TestResourceLoaderRelativeRoot pins that a relative --config behaves exactly
// like the absolute path to the same tree. "octo run --config ." — and
// "--config config.yaml", which configDir reduces to "." — used to fail every
// template with `resource id "templates/p.tmpl" escapes the resource root`:
// Clean(".") is ".", and resolve's Join folds the leading "./" back off its
// result, so the prefix test could never match. Every other test here roots the
// loader at t.TempDir(), which is absolute, and so cannot see this.
func TestResourceLoaderRelativeRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "templates", "p.tmpl"), "hi")
	// A file just outside the tree: containment must still hold from a relative root.
	writeFile(t, filepath.Join(filepath.Dir(root), "secret"), "top")
	t.Chdir(root)

	for _, spec := range []string{".", "./", "./templates/.."} {
		loader := newResourceLoader(spec)
		data, err := loader.Load(context.Background(), core.ResourceKindTemplate, "templates/p.tmpl")
		if err != nil {
			t.Fatalf("Load with root %q: %v", spec, err)
		}
		if string(data) != "hi" {
			t.Fatalf("Load with root %q = %q, want %q", spec, data, "hi")
		}
		if out, err := loader.Load(context.Background(), core.ResourceKindEnv, "../secret"); err == nil {
			t.Fatalf("root %q: traversal returned %q, want an error", spec, out)
		}
	}
}

func TestResourceLoaderLoadMissing(t *testing.T) {
	loader := newResourceLoader(t.TempDir())
	_, err := loader.Load(context.Background(), core.ResourceKindEnv, ".env.nope")
	if !errors.Is(err, core.ErrResourceNotFound) {
		t.Fatalf("Load missing err = %v, want ErrResourceNotFound", err)
	}
}

func TestResourceLoaderRejectsTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cfg")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	// A secret sitting outside the root must not be reachable via "..".
	writeFile(t, filepath.Join(filepath.Dir(root), "secret"), "top")

	loader := newResourceLoader(root)
	if _, err := loader.Load(context.Background(), core.ResourceKindEnv, "../secret"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestResourceLoaderOnChangeFires(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env.dev"), "A=1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes := make(chan core.ResourceKind, 8)
	loader := newResourceLoader(root)
	if err := loader.OnChange(ctx, func(kind core.ResourceKind, _ string) { changes <- kind }); err != nil {
		t.Fatalf("OnChange: %v", err)
	}

	// Give the watcher a moment to register, then modify the file.
	time.Sleep(50 * time.Millisecond)
	writeFile(t, filepath.Join(root, ".env.dev"), "A=2")

	select {
	case kind := <-changes:
		if kind != core.ResourceKindEnv {
			t.Fatalf("change kind = %q, want env", kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnChange did not fire on a file write")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
