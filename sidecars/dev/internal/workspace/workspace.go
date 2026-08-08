// Package workspace owns the one directory the octo runtime watches, and is the
// only writer to it.
//
// Three primitives here are Go ports of ones that already exist in
// packages/run-host, where the editor's local runner solves the same problem
// against a per-namespace directory. Their reasoning is carried over rather than
// rediscovered:
//
//   - Atomic write (write a sibling temp file, then rename) so the runtime's
//     watcher observes a single event per file rather than a partially written
//     one — writeConfig, packages/run-host/src/session.ts:339.
//   - Resources staged BESIDE the config, because the runtime's resource loader
//     is rooted at the config directory — resolveAndStage,
//     packages/run-host/src/resources.ts.
//   - Path containment on every declared resource name, mirroring the runtime's
//     own loader: clean the name as an absolute path so '..' cannot climb out,
//     then assert the result is still under the workspace — stagedPathFor, same
//     file.
//   - Files no longer declared are pruned, because env resources hold secrets and
//     a leftover one is a credential nobody knows is on disk — sync,
//     packages/run-host/src/session.ts:482-493.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConfigFileName is what the integration's definition is written as.
//
// The runtime is pointed at the workspace DIRECTORY, and a directory config
// merges every *.yaml/*.yml file in it into one document
// (runtime/core/runtime/config.go:36). That is what makes the prune in Apply a
// correctness requirement and not just hygiene: a leftover config file from an
// earlier generation would not be ignored, it would be merged into the next one.
// The name deliberately avoids the runtime's _test suffix, which it skips.
const ConfigFileName = "integration.yaml"

// filePerm is the mode staged files get. Env resources carry an integration's
// secrets, so nothing wider than owner-only is appropriate; the runtime container
// beside us runs as the same uid, which is what makes owner-only sufficient.
const filePerm fs.FileMode = 0o600

// dirPerm is the mode created directories get: owner-only for the same reason,
// plus execute so the paths beneath them can be traversed.
const dirPerm fs.FileMode = 0o700

// tempPattern names the sibling temp files an atomic write goes through. The
// leading dot keeps them out of a casual listing, and the shared prefix is what
// lets a prune recognise (and clean up) one left behind by a crash mid-write.
const tempPattern = ".tmp-*"

// ErrEscapes reports a declared resource name that does not stay inside the
// workspace once resolved.
var ErrEscapes = errors.New("resource name escapes the workspace")

// File is one file to stage: a declared resource name (relative, possibly
// containing '/') and its contents.
type File struct {
	Name    string
	Content string
}

// Report says what an Apply did, so a caller can log it and /status can report it
// without re-reading the directory.
type Report struct {
	// Staged is the number of resource files written, config excluded.
	Staged int
	// Pruned lists the workspace-relative paths removed because nothing declared
	// them any more.
	Pruned []string
}

// Workspace is a directory the runtime watches.
type Workspace struct {
	dir string
}

// New returns a Workspace over dir. The directory is not touched until Ensure.
func New(dir string) *Workspace {
	return &Workspace{dir: filepath.Clean(dir)}
}

// Dir returns the directory being managed.
func (w *Workspace) Dir() string { return w.dir }

// Ensure creates the workspace directory if it is missing.
//
// This must happen before the runtime container starts, and it is not a
// convenience. `octo run --watch` tolerates a missing or invalid CONFIG — it logs
// the load error, goes to a reloading state, and waits for the next change
// (runtime/octo/runcmd.go:180). It does not tolerate a missing DIRECTORY: the
// watcher's fsnotify Add fails and the process exits (runtime/octo/watch.go:29).
// So an empty workspace is a working starting point and an absent one is a
// crash-loop.
func (w *Workspace) Ensure() error {
	if err := os.MkdirAll(w.dir, dirPerm); err != nil {
		return fmt.Errorf("create workspace %q: %w", w.dir, err)
	}
	return nil
}

// Apply makes the workspace hold exactly this definition and these resources.
//
// The order of operations is load-bearing:
//
//  1. Prune first. A stale *.yaml would otherwise be merged into the very
//     generation step 3 triggers, and a stale env file would still be readable by
//     it. Doing this before the new config lands means the reload in step 3 reads
//     a directory with nothing extra in it.
//  2. Stage the resources, so they are in place before anything reads them.
//  3. Write the config LAST. Every write is an event on the watched directory and
//     the runtime debounces them for 200ms (runtime/octo/watch.go:19), so the
//     config write is both the final event and the one that guarantees a reload
//     happens after the workspace is complete.
//
// The window between steps 1 and 3 is the one place this can be observed
// mid-flight: if a burst of writes outlasts the debounce, the runtime may load an
// old config whose resources have just been removed. That load fails, the runtime
// logs it and waits, and step 3 then drives a correct reload — which is precisely
// the failure mode `--watch` is built to absorb, and the reason to prefer it over
// leaving a stale config readable.
//
// A failed Apply leaves the workspace usable rather than half-built: prune only
// removes what the new bundle does not declare, and each write is atomic, so the
// runtime never sees a truncated file.
func (w *Workspace) Apply(definition string, files []File) (Report, error) {
	if err := w.Ensure(); err != nil {
		return Report{}, err
	}

	// Resolve every name up front, so a bad one fails before anything is written.
	targets := make(map[string]string, len(files)) // absolute path -> content
	for _, f := range files {
		path, err := w.resolve(f.Name)
		if err != nil {
			return Report{}, err
		}
		targets[path] = f.Content
	}

	keep := make(map[string]struct{}, len(targets)+1)
	keep[filepath.Join(w.dir, ConfigFileName)] = struct{}{}
	for path := range targets {
		keep[path] = struct{}{}
	}

	pruned, err := w.prune(keep)
	if err != nil {
		return Report{}, err
	}

	for path, content := range targets {
		if err := w.write(path, content); err != nil {
			return Report{Pruned: pruned}, err
		}
	}

	if err := w.write(filepath.Join(w.dir, ConfigFileName), definition); err != nil {
		return Report{Staged: len(targets), Pruned: pruned}, err
	}
	return Report{Staged: len(targets), Pruned: pruned}, nil
}

// RemoveConfig deletes the config, leaving the workspace present but empty of
// anything to run. That is a "stop" the runtime can observe without the pod being
// killed: the watcher fires, the load finds no config, and the runtime reloads to
// an empty generation and waits. Absent config is not an error.
func (w *Workspace) RemoveConfig() error {
	path := filepath.Join(w.dir, ConfigFileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove config: %w", err)
	}
	return nil
}

// List returns the workspace-relative paths of every file present, sorted. For
// diagnostics: "what does the runtime actually see?" is the first question when a
// reload did not do what someone expected.
func (w *Workspace) List() ([]string, error) {
	var out []string
	err := filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(w.dir, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list workspace: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// resolve maps a declared resource name to an absolute path inside the workspace,
// mirroring the runtime loader's containment: clean the name as if it were
// absolute (which strips any leading '..' segments), rejoin it under the
// workspace, then assert the result really is under it.
//
// A name that resolves onto the config file is rejected too. Nothing legitimate
// does that, and allowing it would let a resource silently replace the definition
// between the prune and the config write.
//
// Any other *.yaml/*.yml name is rejected for the same reason one step removed: the
// runtime merges every *.yaml/*.yml in the directory into the generation (see
// ConfigFileName), so such a resource would not sit beside the config as data — it
// would become part of the config. Keeping the definition the only YAML the runtime
// loads is the invariant this preserves.
func (w *Workspace) resolve(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty name", ErrEscapes)
	}
	cleaned := filepath.Clean("/" + filepath.FromSlash(trimmed))
	path := filepath.Join(w.dir, cleaned)
	if path != w.dir && !strings.HasPrefix(path, w.dir+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrEscapes, name)
	}
	if path == w.dir {
		return "", fmt.Errorf("%w: %q names the workspace itself", ErrEscapes, name)
	}
	if path == filepath.Join(w.dir, ConfigFileName) {
		return "", fmt.Errorf("%w: %q collides with the config file", ErrEscapes, name)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return "", fmt.Errorf("%w: %q would be merged into the runtime config", ErrEscapes, name)
	}
	return path, nil
}

// write atomically replaces path's contents: a sibling temp file in the same
// directory (so the rename cannot cross filesystems), then rename over the
// target. The runtime's watcher sees one event, and never a partial file.
func (w *Workspace) write(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("create temp for %q: %w", path, err)
	}
	tmpName := tmp.Name()
	// Any failure from here on leaves a temp file behind; remove it rather than
	// leaning on the next prune, so a transient error does not litter the directory
	// the runtime is watching.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %q: %w", path, err)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp for %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %q: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

// prune removes every file in the workspace that is not in keep, then any
// directory left empty by that. Returns the workspace-relative paths removed.
//
// Scanning the directory rather than tracking what was written last time is
// deliberate: the sidecar can restart while the pod lives on, and a prune that
// depends on remembered state would silently stop cleaning after such a restart —
// leaving exactly the stale secret, or the stale merged *.yaml, this exists to
// prevent.
func (w *Workspace) prune(keep map[string]struct{}) ([]string, error) {
	var (
		removed []string
		dirs    []string
	)
	err := filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != w.dir {
				dirs = append(dirs, path)
			}
			return nil
		}
		if _, ok := keep[path]; ok {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return fmt.Errorf("prune %q: %w", path, rmErr)
		}
		if rel, relErr := filepath.Rel(w.dir, path); relErr == nil {
			removed = append(removed, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	// Deepest first, so a directory emptied by removing its children is itself
	// removable. A non-empty directory simply fails and is left alone, which is the
	// wanted behaviour and cheaper than checking first.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}

	sort.Strings(removed)
	return removed, nil
}
