package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// A file just outside the tree, and one inside it under the same name:
	// containment must still hold from a relative root, and must hold by refusing
	// the id rather than by quietly resolving it to the inner file.
	writeFile(t, filepath.Join(filepath.Dir(root), "secret"), "top")
	writeFile(t, filepath.Join(root, "secret"), "inside")
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
		out, err := loader.Load(context.Background(), core.ResourceKindEnv, "../secret")
		if err == nil {
			t.Fatalf("root %q: traversal returned %q, want an error", spec, out)
		}
		if !strings.Contains(err.Error(), "escapes the resource root") {
			t.Fatalf("root %q: traversal err = %v, want the containment guard", spec, err)
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
	// One inside the root too, so the assertion below has teeth: a resolver that
	// clamps "../secret" back under the root would load this and succeed. Without
	// it the test passes on a missing file and proves nothing about containment.
	writeFile(t, filepath.Join(root, "secret"), "inside")

	loader := newResourceLoader(root)
	out, err := loader.Load(context.Background(), core.ResourceKindEnv, "../secret")
	if err == nil {
		t.Fatalf("traversal returned %q, want an error", out)
	}
	if !strings.Contains(err.Error(), "escapes the resource root") {
		t.Fatalf("traversal err = %v, want the containment guard", err)
	}
}

// TestResourceLoaderResolveAtFilesystemRoot covers a root with nothing above it.
// A "root + separator" prefix test cannot admit its own children there — "/" plus
// a separator is "//", which prefixes nothing — so every id under it read as an
// escape. Rare for a config directory, but it is the same check every id runs.
func TestResourceLoaderResolveAtFilesystemRoot(t *testing.T) {
	loader := newResourceLoader(string(os.PathSeparator))
	got, err := loader.resolve("templates/p.tmpl")
	if err != nil {
		t.Fatalf("resolve under the filesystem root: %v", err)
	}
	if want := filepath.Join(loader.root, "templates", "p.tmpl"); got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

// TestResourceLoaderAllowsInnerDotDot pins the other side of the guard: a ".."
// that stays inside the root is an ordinary path, not an escape.
func TestResourceLoaderAllowsInnerDotDot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "templates", "p.tmpl"), "hi")

	loader := newResourceLoader(root)
	data, err := loader.Load(context.Background(), core.ResourceKindTemplate, "partials/../templates/p.tmpl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("Load = %q, want %q", data, "hi")
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
