package standalone

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"

	"github.com/juancavallotti/octo/runtime/core"
)

// fsResourceLoader is the standalone module's core.ResourceLoader: it serves
// resources from the filesystem, rooted at the config file's directory, so a
// resource id maps directly to a relative path under that root. The env resource
// ".env.dev" resolves to "<root>/.env.dev"; the template "templates/welcome.tmpl"
// resolves to "<root>/templates/welcome.tmpl". Ids are confined to the root: an
// id escaping it via ".." is rejected lexically, and one escaping it through a
// symlink is rejected by the kernel, because the read goes through an os.Root.
// It also implements core.ResourceWatcher, notifying of changes to any file under
// the root.
type fsResourceLoader struct {
	root string
}

// newResourceLoader returns a filesystem loader rooted at root — the config
// directory. root is made absolute once (Abs cleans too) so every downstream
// comparison is between absolute paths. A relative root is not merely untidy: it
// is wrong. "--config ." cleans to ".", and resolve's Join then folds the "./"
// away, so "templates/p.tmpl" no longer has the root as a prefix and every id
// reads as an escape.
func newResourceLoader(root string) *fsResourceLoader {
	abs, err := filepath.Abs(root)
	if err != nil {
		// Abs only fails when the working directory is unreadable. Fall back to the
		// cleaned path rather than refusing to build a loader at all.
		slog.Warn("resource root could not be made absolute", "root", root, "error", err)
		abs = filepath.Clean(root)
	}
	return &fsResourceLoader{root: abs}
}

// Load reads the resource id under the root. kind is ignored: the id alone maps to
// a path. A file that does not exist yields core.ErrResourceNotFound; an id that
// escapes the root, or an unreadable file, yields a real error.
//
// The read goes through os.Root rather than os.ReadFile because resolve's answer
// is lexical, and a lexical answer is not the whole containment question: if
// "<root>/link" is a symlink to somewhere else, "link/secret" is a path under the
// root that names a file outside it. os.Root resolves each component against the
// opened root directory and refuses the ones that leave, so the escape is decided
// by the kernel on the path actually walked, not by the string. A relative
// symlink landing inside the root is followed as usual; an absolute one is
// refused even where it points back inside, which is os.Root's rule.
func (l *fsResourceLoader) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	rel, err := l.resolve(id)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(l.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No root directory at all: every resource under it is missing, which is
			// the answer a caller resolving an optional resource expects.
			return nil, core.ErrResourceNotFound
		}
		return nil, fmt.Errorf("open resource root %q: %w", l.root, err)
	}
	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, core.ErrResourceNotFound
		}
		return nil, fmt.Errorf("load resource %q: %w", id, err)
	}
	return data, nil
}

// resolve maps a resource id to a cleaned path relative to the root, rejecting
// any id that escapes it. Containment is decided by filepath.Rel: a relative path
// that starts with ".." left the root, anything else did not. The result is
// relative because Load reads it through an os.Root, which resolves it against
// the root directory itself.
//
// Rel rather than a "root + separator" prefix test, on the joined-but-unclamped
// path, for two reasons. A root that is itself a filesystem root has no such
// prefix — "/" plus a separator is "//", which prefixes none of its own children
// — so the assertion refused every id under it. And pre-cleaning the id as an
// absolute path ("/" + id) silently rewrote "../secret" to "<root>/secret"
// instead of refusing it, which is safe but answers a different question than the
// one asked: the guard could then never fire, and the id resolved somewhere the
// caller did not name. A ".." that stays inside the root is still fine — Join
// folds it away here, so os.Root never has to walk through a directory the id
// only mentioned on its way back out.
func (l *fsResourceLoader) resolve(id string) (string, error) {
	full := filepath.Join(l.root, id)
	rel, err := filepath.Rel(l.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("resource id %q escapes the resource root", id)
	}
	return rel, nil
}

// OnChange watches the root subtree and calls fn for every file change, mapping
// the changed path back to its (kind, id). It watches until ctx is done. New
// subdirectories created under the root are watched as they appear, so files added
// later are still observed.
func (l *fsResourceLoader) OnChange(ctx context.Context, fn core.ChangeFunc) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("new resource watcher: %w", err)
	}
	if err := addTree(watcher, l.root); err != nil {
		_ = watcher.Close()
		return err
	}
	go l.watchLoop(ctx, watcher, fn)
	return nil
}

// watchLoop forwards file-change events to fn until ctx is cancelled or the
// watcher closes, then closes the watcher.
func (l *fsResourceLoader) watchLoop(ctx context.Context, watcher *fsnotify.Watcher, fn core.ChangeFunc) {
	defer func() { _ = watcher.Close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			l.handleEvent(watcher, event, fn)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("resource watcher error", "error", err)
		}
	}
}

// handleEvent watches newly created subdirectories and reports file changes to fn.
// Directory events themselves are ignored (only files are resources).
func (l *fsResourceLoader) handleEvent(watcher *fsnotify.Watcher, event fsnotify.Event, fn core.ChangeFunc) {
	info, statErr := os.Stat(event.Name)
	isDir := statErr == nil && info.IsDir()
	if event.Op&fsnotify.Create != 0 && isDir {
		// A new subdirectory: watch it so files added under it later are observed.
		_ = watcher.Add(event.Name)
		return
	}
	if isDir {
		return
	}
	id, ok := l.idForPath(event.Name)
	if !ok {
		return
	}
	fn(kindForID(id), id)
}

// idForPath returns the resource id (root-relative, slash-separated) for an
// absolute path, reporting false when the path is not under the root.
func (l *fsResourceLoader) idForPath(path string) (string, bool) {
	rel, err := filepath.Rel(l.root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// kindForID classifies a resource id by the .env naming convention: a base name
// starting with ".env" is an env resource, everything else is a template.
func kindForID(id string) core.ResourceKind {
	if strings.HasPrefix(filepath.Base(id), ".env") {
		return core.ResourceKindEnv
	}
	return core.ResourceKindTemplate
}

// addTree registers root and every subdirectory under it with the watcher, so
// changes anywhere in the subtree are observed (fsnotify watches are not recursive).
func addTree(watcher *fsnotify.Watcher, root string) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if addErr := watcher.Add(path); addErr != nil {
			return fmt.Errorf("watch %q: %w", path, addErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk resource tree %q: %w", root, err)
	}
	return nil
}
