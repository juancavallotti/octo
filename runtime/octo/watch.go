package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/runtime"
)

// watchDebounce is how long to wait after the last filesystem event before
// signalling a change, so a burst of editor writes coalesces into one reload.
const watchDebounce = 200 * time.Millisecond

// watchConfig watches the config path for changes, emitting on the returned
// channel (debounced) whenever the config changes — see watchScope for what that
// covers. When path is a directory, config files added to or removed from it are
// noticed too. The .env files consulted during loading are watched as well, so
// editing them triggers the same full restart as a config change. When resources
// is non-nil and supports change notification, its resource edits feed the same
// debounced channel, so an imported env file or template change reloads too —
// but only for a resource the running generation read, as recorded in reads. The
// watcher runs until ctx is cancelled.
func watchConfig(
	ctx context.Context, path string, resources core.ResourceLoader, reads *resourceReads,
) (<-chan struct{}, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("new watcher: %w", err)
	}

	scope, err := newWatchScope(path, runtime.DotEnvPaths())
	if err != nil {
		_ = watcher.Close()
		return nil, err
	}
	for _, target := range scope.targets() {
		if err := watcher.Add(target); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watch %q: %w", target, err)
		}
	}

	changed := make(chan struct{}, 1)

	// A resource loader that opts into change notification feeds its events into a
	// side channel the watch loop debounces alongside filesystem events. A loader
	// that is not a ResourceWatcher (e.g. the k8s no-op) contributes nothing.
	var resourceCh chan struct{}
	if w, ok := resources.(core.ResourceWatcher); ok {
		resourceCh = make(chan struct{}, 1)
		onChange := func(_ core.ResourceKind, id string) {
			if reads.has(id) {
				notify(resourceCh)
			}
		}
		if err := w.OnChange(ctx, onChange); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watch resources: %w", err)
		}
	}

	go watchLoop(ctx, watcher, scope, resourceCh, changed)
	return changed, nil
}

// watchScope is what a reload depends on: the config file or directory and the
// .env files consulted while loading it.
//
// The watcher has to register directories rather than files, because an editor
// that saves by writing a temporary file and renaming it over the original
// replaces the file a file-level watch was attached to. A directory sees every
// write inside it, though — the working directory, where ./.env lives, is also
// where a redirected log or the default trace file is written — so each event is
// checked against the files that actually matter before it counts as a change.
// Without that, a runtime writing into its own working directory reloads itself
// in a loop.
type watchScope struct {
	// files are the individual files a change to which reloads.
	files map[string]struct{}
	// configDir is set when the config is a directory: any file in it the loader
	// would read counts.
	configDir string
}

// newWatchScope resolves the config path and .env paths to absolute paths, so
// they compare equal to the event names fsnotify reports for the directories
// registered from them.
func newWatchScope(configPath string, dotEnvPaths []string) (watchScope, error) {
	scope := watchScope{files: map[string]struct{}{}}
	config, err := filepath.Abs(configPath)
	if err != nil {
		return watchScope{}, fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	if info, statErr := os.Stat(config); statErr == nil && info.IsDir() {
		scope.configDir = config
	} else {
		scope.files[config] = struct{}{}
	}
	for _, path := range dotEnvPaths {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return watchScope{}, fmt.Errorf("resolve .env path %q: %w", path, absErr)
		}
		scope.files[abs] = struct{}{}
	}
	return scope, nil
}

// targets returns the directories to register, once each. A .env whose directory
// does not exist is skipped: the file cannot appear there.
func (s watchScope) targets() []string {
	seen := map[string]struct{}{}
	var ret []string
	add := func(dir string) {
		if _, dup := seen[dir]; dup {
			return
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return
		}
		seen[dir] = struct{}{}
		ret = append(ret, dir)
	}
	if s.configDir != "" {
		add(s.configDir)
	}
	for file := range s.files {
		add(filepath.Dir(file))
	}
	slices.Sort(ret)
	return ret
}

// covers reports whether an event on name is a change a reload depends on.
func (s watchScope) covers(name string) bool {
	name = filepath.Clean(name)
	if _, ok := s.files[name]; ok {
		return true
	}
	return s.configDir != "" && filepath.Dir(name) == s.configDir && runtime.IsConfigFile(filepath.Base(name))
}

// watchLoop forwards debounced change notifications until ctx is cancelled or the
// watcher closes, then closes the watcher. Filesystem events and resource-change
// events (resourceCh, nil when no ResourceWatcher is wired) share one debounce
// timer, so a burst across both coalesces into a single reload.
func watchLoop(
	ctx context.Context, watcher *fsnotify.Watcher, scope watchScope, resourceCh <-chan struct{},
	changed chan<- struct{},
) {
	defer func() { _ = watcher.Close() }()
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if scope.covers(event.Name) {
				timer, timerC = armDebounce(timer)
			}
		case <-resourceCh:
			timer, timerC = armDebounce(timer)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("config watcher error", "error", err)
		case <-timerC:
			timer, timerC = nil, nil
			notify(changed)
		}
	}
}

// armDebounce (re)starts the debounce timer, returning it and its channel.
func armDebounce(timer *time.Timer) (*time.Timer, <-chan time.Time) {
	if timer == nil {
		timer = time.NewTimer(watchDebounce)
	} else {
		timer.Reset(watchDebounce)
	}
	return timer, timer.C
}

// notify sends a non-blocking change signal (coalescing if one is pending).
func notify(changed chan<- struct{}) {
	select {
	case changed <- struct{}{}:
	default:
	}
}

// resourceReads records which resources the current generation read, so a
// resource change reloads only when it can change what is running.
//
// The resource loader notifies of every file under its root, and the root is the
// config directory — where a redirected log or the default trace file is written
// when the runtime runs from there. Declared resources are not the whole set
// either: a block may name a template by id without declaring it. Every read
// goes through the loader, though, so recording reads catches both, and a file
// nothing has read yet is read fresh the first time it is used.
type resourceReads struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func newResourceReads() *resourceReads {
	return &resourceReads{ids: map[string]struct{}{}}
}

// reset forgets the previous generation's reads; the next one records its own.
func (r *resourceReads) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.ids)
}

func (r *resourceReads) record(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids[resourceKey(id)] = struct{}{}
}

func (r *resourceReads) has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.ids[resourceKey(id)]
	return ok
}

// resourceKey puts an id in the form the loader reports changes in: slash
// separated and relative to the root, so "./templates/a.tmpl" and the
// notification for that file meet.
func resourceKey(id string) string {
	return strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(id)), "/")
}

// services returns svc with its resource loader recording every read, missing
// files included: a declared resource created later is a change worth a reload.
//
//nolint:ireturn // the generation runs against the RuntimeServices interface
func (r *resourceReads) services(svc core.RuntimeServices) core.RuntimeServices {
	return recordingServices{RuntimeServices: svc, loader: recordingLoader{ResourceLoader: svc.Resources(), reads: r}}
}

type recordingServices struct {
	core.RuntimeServices
	loader recordingLoader
}

//nolint:ireturn // implements core.RuntimeServices
func (s recordingServices) Resources() core.ResourceLoader { return s.loader }

// LogSink carries the optional core.LogShipper facet through the wrapper, which
// embedding the interface alone would hide from the logger connector. A nil
// handler is how a module that ships no logs answers, so it stands in for a
// module that does not implement the facet at all.
//
//nolint:ireturn // implements core.LogShipper
func (s recordingServices) LogSink() slog.Handler {
	if shipper, ok := s.RuntimeServices.(core.LogShipper); ok {
		return shipper.LogSink()
	}
	return nil
}

type recordingLoader struct {
	core.ResourceLoader
	reads *resourceReads
}

func (l recordingLoader) Load(ctx context.Context, kind core.ResourceKind, id string) ([]byte, error) {
	l.reads.record(id)
	return l.ResourceLoader.Load(ctx, kind, id) //nolint:wrapcheck // the loader's own error, passed through
}
