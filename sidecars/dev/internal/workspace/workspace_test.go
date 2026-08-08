package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// read returns the contents of a workspace-relative path, failing the test if it
// cannot be read.
func read(t *testing.T, w *Workspace, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.Dir(), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// list returns the workspace listing, failing the test on error.
func list(t *testing.T, w *Workspace) []string {
	t.Helper()
	got, err := w.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func TestApplyWritesConfigAndResources(t *testing.T) {
	w := New(t.TempDir())

	report, err := w.Apply("name: demo\n", []File{
		{Name: ".env.dev", Content: "A=1\n"},
		{Name: "templates/welcome.tmpl", Content: "hi\n"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if report.Staged != 2 {
		t.Errorf("Staged = %d, want 2", report.Staged)
	}

	// Exactly these files and nothing else — in particular no leftover temp file,
	// which is the visible symptom of a write that did not go through a rename.
	want := []string{".env.dev", ConfigFileName, "templates/welcome.tmpl"}
	if got := list(t, w); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if got := read(t, w, ConfigFileName); got != "name: demo\n" {
		t.Errorf("config = %q", got)
	}
	if got := read(t, w, ".env.dev"); got != "A=1\n" {
		t.Errorf("env = %q", got)
	}
	if got := read(t, w, "templates/welcome.tmpl"); got != "hi\n" {
		t.Errorf("template = %q", got)
	}
}

// Env resources hold an integration's secrets, so one that is no longer declared
// must not stay readable on disk.
func TestApplyPrunesUndeclaredResources(t *testing.T) {
	w := New(t.TempDir())

	if _, err := w.Apply("v1\n", []File{
		{Name: ".env.dev", Content: "A=1\n"},
		{Name: "secrets/.env.old", Content: "TOKEN=shh\n"},
	}); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	report, err := w.Apply("v2\n", []File{{Name: ".env.dev", Content: "A=2\n"}})
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	if want := []string{"secrets/.env.old"}; !reflect.DeepEqual(report.Pruned, want) {
		t.Errorf("Pruned = %v, want %v", report.Pruned, want)
	}
	if got := list(t, w); !reflect.DeepEqual(got, []string{".env.dev", ConfigFileName}) {
		t.Errorf("files = %v, want the config and .env.dev only", got)
	}
	// The directory the pruned file lived in should go too, so the workspace does not
	// accumulate empty scaffolding.
	if _, err := os.Stat(filepath.Join(w.Dir(), "secrets")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat secrets/: %v, want it removed once empty", err)
	}
	if got := read(t, w, ".env.dev"); got != "A=2\n" {
		t.Errorf("env = %q, want the new content", got)
	}
}

// The strongest reason the prune exists: a directory config MERGES every *.yaml in
// the directory (runtime/core/runtime/config.go:36), so a leftover one from an
// earlier generation is not ignored — it becomes part of the next generation.
func TestApplyPrunesStaleConfigFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "leftover.yaml")
	if err := os.WriteFile(stale, []byte("flows: []\n"), 0o600); err != nil {
		t.Fatalf("seed stale config: %v", err)
	}

	w := New(dir)
	report, err := w.Apply("name: demo\n", nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"leftover.yaml"}; !reflect.DeepEqual(report.Pruned, want) {
		t.Errorf("Pruned = %v, want %v", report.Pruned, want)
	}
	if got := list(t, w); !reflect.DeepEqual(got, []string{ConfigFileName}) {
		t.Errorf("files = %v, want only the config", got)
	}
}

// The prune scans the directory rather than remembering what it wrote, so it keeps
// working across a sidecar restart within a living pod. A fresh Workspace over a
// dirty directory is exactly that case.
func TestPruneSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(dir).Apply("v1\n", []File{{Name: ".env.dev", Content: "A=1\n"}}); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// A brand-new Workspace: no memory of the previous staging at all.
	report, err := New(dir).Apply("v2\n", nil)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if want := []string{".env.dev"}; !reflect.DeepEqual(report.Pruned, want) {
		t.Errorf("Pruned = %v, want %v", report.Pruned, want)
	}
}

// Traversal is CONTAINED, not rejected, and that is the same choice the runtime
// loader makes (stagedPathFor, packages/run-host/src/resources.ts): cleaning the
// name as if it were absolute strips leading '..' segments, so "../x" simply
// becomes the workspace-relative "x". The property worth asserting is therefore
// not an error — it is that nothing lands outside the workspace.
func TestApplyContainsTraversalNames(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{"parent traversal", "../escaped.env", "escaped.env"},
		{"deep traversal", "a/../../escaped.env", "escaped.env"},
		{"absolute", "/nested/env", "nested/env"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A parent directory of our own, so an escape would be observable rather than
			// landing somewhere the test cannot see.
			root := t.TempDir()
			dir := filepath.Join(root, "integrations")
			w := New(dir)

			if _, err := w.Apply("name: demo\n", []File{{Name: tc.file, Content: "A=1\n"}}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			want := []string{ConfigFileName, tc.want}
			sort.Strings(want) // List is sorted; the expectation has to be too.
			if got := list(t, w); !reflect.DeepEqual(got, want) {
				t.Errorf("files = %v, want %v", got, want)
			}

			// Nothing but the workspace itself may appear beside it.
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read parent: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "integrations" {
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("parent contains %v, want only the workspace dir", names)
			}
		})
	}
}

func TestApplyRejectsInvalidNames(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"names the workspace", "."},
		{"resolves to the workspace", "/"},
		{"collides with the config", ConfigFileName},
		// A directory config merges every *.yaml/*.yml, so a resource with either
		// extension would be loaded as config rather than staged beside it.
		{"a yaml resource", "extra.yaml"},
		{"a yml resource in a subdir", "conf.d/settings.yml"},
		{"a yaml resource in mixed case", "Extra.YAML"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := New(t.TempDir())
			_, err := w.Apply("name: demo\n", []File{{Name: tc.file, Content: "x"}})
			if !errors.Is(err, ErrEscapes) {
				t.Fatalf("err = %v, want ErrEscapes", err)
			}
			// Names are resolved before anything is written, so a rejected bundle leaves
			// the workspace exactly as it was — including not having pruned it.
			if got := list(t, w); len(got) != 0 {
				t.Errorf("files = %v, want none written", got)
			}
		})
	}
}

// A pull that fails is handled by the caller not calling Apply at all; what this
// asserts is the other half — an Apply that fails part way through must not leave
// the runtime looking at a truncated file or a config that lost its resources.
func TestApplyFailureLeavesWorkspaceReadable(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	if _, err := w.Apply("v1\n", []File{{Name: ".env.dev", Content: "A=1\n"}}); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// A resource name that resolves onto a path whose parent is an existing FILE
	// cannot be created, so the write fails after the prune has already run.
	_, err := w.Apply("v2\n", []File{
		{Name: ".env.dev", Content: "A=2\n"},
		{Name: ".env.dev/nope", Content: "x"},
	})
	if err == nil {
		t.Fatal("Apply: want an error")
	}

	// The config is still whole and parseable — either generation is acceptable, a
	// truncated file is not.
	got := read(t, w, ConfigFileName)
	if got != "v1\n" && got != "v2\n" {
		t.Errorf("config = %q, want one whole generation", got)
	}
	for _, name := range list(t, w) {
		if strings.HasPrefix(filepath.Base(name), ".tmp-") {
			t.Errorf("temp file %q left behind by a failed write", name)
		}
	}
}

func TestRemoveConfig(t *testing.T) {
	w := New(t.TempDir())
	if _, err := w.Apply("name: demo\n", []File{{Name: ".env.dev", Content: "A=1\n"}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if err := w.RemoveConfig(); err != nil {
		t.Fatalf("RemoveConfig: %v", err)
	}
	// Only the config goes: the resources stay staged, so a later reload does not
	// have to re-fetch them just to resume.
	if got := list(t, w); !reflect.DeepEqual(got, []string{".env.dev"}) {
		t.Errorf("files = %v, want .env.dev to remain", got)
	}
	// Idempotent: "there is nothing to run" is the goal, and it is already met.
	if err := w.RemoveConfig(); err != nil {
		t.Errorf("second RemoveConfig: %v, want nil", err)
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "integrations")
	w := New(dir)
	for i := range 2 {
		if err := w.Ensure(); err != nil {
			t.Fatalf("Ensure %d: %v", i, err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("stat %s: %v (isDir=%v)", dir, err, info != nil && info.IsDir())
	}
}

// List on a workspace that does not exist yet is empty, not an error: /diagnostics
// is most useful before the first pull, which is exactly when the directory may be
// bare.
func TestListMissingDirectory(t *testing.T) {
	w := New(filepath.Join(t.TempDir(), "absent"))
	got, err := w.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("files = %v, want none", got)
	}
}

// Staged files carry secrets, so the mode is part of the contract rather than an
// accident of however the temp file was created.
func TestStagedFilesAreOwnerOnly(t *testing.T) {
	w := New(t.TempDir())
	if _, err := w.Apply("name: demo\n", []File{{Name: ".env.dev", Content: "A=1\n"}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, rel := range []string{ConfigFileName, ".env.dev"} {
		info, err := os.Stat(filepath.Join(w.Dir(), rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != filePerm {
			t.Errorf("%s mode = %v, want %v", rel, got, filePerm)
		}
	}
}
