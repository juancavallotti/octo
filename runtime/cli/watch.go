package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchDebounce is how long to wait after the last filesystem event before
// signalling a change, so a burst of editor writes coalesces into one reload.
const watchDebounce = 200 * time.Millisecond

// watchConfig watches the config path for changes, emitting on the returned
// channel (debounced) whenever the file or directory contents change. When path
// is a directory the directory itself is watched, so added/removed config files
// are noticed too. The watcher runs until ctx is cancelled.
func watchConfig(ctx context.Context, path string) (<-chan struct{}, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Watch the directory containing the target so atomic-rename saves (common in
	// editors, which replace rather than write in place) are still observed. For a
	// directory target we watch it directly.
	watchTarget := path
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		watchTarget = filepath.Dir(path)
	}
	if err := watcher.Add(watchTarget); err != nil {
		_ = watcher.Close()
		return nil, err
	}

	changed := make(chan struct{}, 1)
	go func() {
		defer watcher.Close()
		var timer *time.Timer
		var timerC <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-watcher.Events:
				if !ok {
					return
				}
				// (Re)arm the debounce timer on every event.
				if timer == nil {
					timer = time.NewTimer(watchDebounce)
					timerC = timer.C
				} else {
					timer.Reset(watchDebounce)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("config watcher error", "error", err)
			case <-timerC:
				timer = nil
				timerC = nil
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}
	}()

	return changed, nil
}
