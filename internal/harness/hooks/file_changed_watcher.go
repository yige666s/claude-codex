package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileChangedWatcher struct {
	executor *Executor
	cwd      string
	interval time.Duration

	mu      sync.Mutex
	paths   []string
	known   map[string]fileSnapshot
	cancel  context.CancelFunc
	running bool
}

type fileSnapshot struct {
	exists bool
	mod    time.Time
	size   int64
}

func NewFileChangedWatcher(executor *Executor, cwd string, paths []string) *FileChangedWatcher {
	w := &FileChangedWatcher{
		executor: executor,
		cwd:      cwd,
		interval: 200 * time.Millisecond,
		paths:    normalizeWatchPaths(cwd, paths),
		known:    map[string]fileSnapshot{},
	}
	w.snapshotLocked()
	return w
}

func (w *FileChangedWatcher) SetInterval(interval time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if interval > 0 {
		w.interval = interval
	}
}

func (w *FileChangedWatcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.running = true
	w.snapshotLocked()
	interval := w.interval
	w.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Poll(ctx)
			}
		}
	}()
}

func (w *FileChangedWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
	w.cancel = nil
	w.running = false
}

func (w *FileChangedWatcher) UpdateWatchPaths(paths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paths = normalizeWatchPaths(w.cwd, paths)
	w.snapshotLocked()
}

func (w *FileChangedWatcher) Poll(ctx context.Context) {
	w.mu.Lock()
	paths := append([]string(nil), w.paths...)
	previous := make(map[string]fileSnapshot, len(w.known))
	for k, v := range w.known {
		previous[k] = v
	}
	w.mu.Unlock()

	var dynamic []string
	for _, path := range paths {
		next := statWatchPath(path)
		prev, seen := previous[path]
		if !seen {
			w.setSnapshot(path, next)
			continue
		}
		event := ""
		switch {
		case !prev.exists && next.exists:
			event = "add"
		case prev.exists && !next.exists:
			event = "unlink"
		case prev.exists && next.exists && (!prev.mod.Equal(next.mod) || prev.size != next.size):
			event = "change"
		}
		w.setSnapshot(path, next)
		if event == "" || w.executor == nil {
			continue
		}
		result, err := w.executor.Execute(ctx, EventFileChanged, &HookInput{
			Event:      EventFileChanged,
			WorkingDir: w.cwd,
			Metadata: map[string]any{
				"path":  path,
				"event": event,
			},
		})
		if err == nil && result != nil && len(result.WatchPaths) > 0 {
			dynamic = append(dynamic, result.WatchPaths...)
		}
	}
	if len(dynamic) > 0 {
		w.UpdateWatchPaths(append(paths, dynamic...))
	}
}

func (w *FileChangedWatcher) setSnapshot(path string, snapshot fileSnapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.known[path] = snapshot
}

func (w *FileChangedWatcher) snapshotLocked() {
	w.known = map[string]fileSnapshot{}
	for _, path := range w.paths {
		w.known[path] = statWatchPath(path)
	}
}

func normalizeWatchPaths(cwd string, paths []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range paths {
		for _, part := range strings.Split(raw, "|") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !filepath.IsAbs(part) {
				part = filepath.Join(cwd, part)
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

func statWatchPath(path string) fileSnapshot {
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{}
	}
	return fileSnapshot{exists: true, mod: info.ModTime(), size: info.Size()}
}
