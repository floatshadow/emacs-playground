package preview

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// CoalesceWindow is the per-path coalescing window (backend.md §3.10).
const CoalesceWindow = 200 * time.Millisecond

// IgnoredDirs are directory names never watched (backend.md §3.10).
var IgnoredDirs = map[string]bool{
	".git": true, "target": true, "_build": true, ".venv": true, "node_modules": true, ".playground": true,
}

// Ignored reports whether a workspace-relative slash path is outside the
// watch (any segment is an ignored directory, or the base name is an Emacs
// lock file).
func Ignored(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if IgnoredDirs[seg] {
			return true
		}
	}
	return strings.HasPrefix(filepath.Base(rel), ".#")
}

// Watcher watches the workspace recursively and emits coalesced file
// events.
type Watcher struct {
	root   string
	fw     *fsnotify.Watcher
	emit   func(*protocol.FileEvent)
	log    *slog.Logger
	window time.Duration

	// OnGit, when set before Run, is called (coalesced like file events)
	// when the top level of a nested repository's .git changes: a commit,
	// staging, a checkout. Those changes are not file events, but shell
	// conditions that read git must see them.
	OnGit    func()
	gitTimer *time.Timer

	mu      sync.Mutex
	pending map[string]*pendingEvent
	closed  bool
	warned  bool
}

type pendingEvent struct {
	kind  protocol.FileEventKind // "" means "nothing happened" (created+deleted)
	timer *time.Timer
}

// NewWatcher starts watching root. emit is called from timer goroutines and
// must be safe for concurrent use.
func NewWatcher(root string, emit func(*protocol.FileEvent), log *slog.Logger) (*Watcher, error) {
	if log == nil {
		log = slog.Default()
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{root: filepath.Clean(root), fw: fw, emit: emit, log: log, window: CoalesceWindow, pending: map[string]*pendingEvent{}}
	w.addTree(w.root, false)
	return w, nil
}

// SetWindow overrides the coalescing window (tests).
func (w *Watcher) SetWindow(d time.Duration) { w.window = d }

// Run processes fsnotify events until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			w.log.Warn("file watcher", "err", err)
		}
	}
}

// Close stops watching and drops pending events.
func (w *Watcher) Close() error {
	w.mu.Lock()
	w.closed = true
	for _, p := range w.pending {
		p.timer.Stop()
	}
	w.pending = map[string]*pendingEvent{}
	w.mu.Unlock()
	return w.fw.Close()
}

func (w *Watcher) rel(p string) (string, bool) {
	r, err := filepath.Rel(w.root, p)
	if err != nil || r == "." || strings.HasPrefix(r, "..") {
		return "", false
	}
	return filepath.ToSlash(r), true
}

// addTree watches dir and its subdirectories. With announce, files found
// are reported as created (a directory moved or extracted into the
// workspace before its watch existed).
func (w *Watcher) addTree(dir string, announce bool) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, ok := w.rel(p)
		if ok && d.IsDir() && d.Name() == ".git" && rel != ".git" && !Ignored(path.Dir(rel)) {
			// A nested repository: its top level only (nestedGit).
			_ = w.fw.Add(p)
			return fs.SkipDir
		}
		if ok && Ignored(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if err := w.fw.Add(p); err != nil {
				w.mu.Lock()
				warned := w.warned
				w.warned = true
				w.mu.Unlock()
				if !warned {
					w.log.Warn("file watcher: cannot watch directory", "dir", rel, "err", err)
				}
			}
			return nil
		}
		if announce && ok && d.Type().IsRegular() {
			w.record(rel, protocol.FileCreated)
		}
		return nil
	})
}

// nestedGit reports whether rel is directly inside a nested repository's
// .git directory ("projects/hello/.git/index").
func nestedGit(rel string) bool {
	dir := path.Dir(rel)
	return path.Base(dir) == ".git" && dir != ".git" && !Ignored(path.Dir(dir))
}

func (w *Watcher) handle(ev fsnotify.Event) {
	rel, ok := w.rel(ev.Name)
	if !ok {
		return
	}
	if nestedGit(rel) {
		w.gitChanged()
		return
	}
	if ev.Has(fsnotify.Create) && path.Base(rel) == ".git" && !Ignored(path.Dir(rel)) {
		if st, err := os.Lstat(ev.Name); err == nil && st.IsDir() {
			_ = w.fw.Add(ev.Name) // git init in the workspace
			w.gitChanged()
			return
		}
	}
	if Ignored(rel) {
		return
	}
	switch {
	case ev.Has(fsnotify.Create):
		st, err := os.Lstat(ev.Name)
		if err == nil && st.IsDir() {
			w.addTree(ev.Name, true)
			return
		}
		// A file that is already gone still records "created" so that the
		// matching remove cancels it within the window.
		w.record(rel, protocol.FileCreated)
	case ev.Has(fsnotify.Write):
		w.record(rel, protocol.FileModified)
	case ev.Has(fsnotify.Remove), ev.Has(fsnotify.Rename):
		w.record(rel, protocol.FileDeleted)
	}
}

// gitChanged calls OnGit once per coalescing window.
func (w *Watcher) gitChanged() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.OnGit == nil || w.closed || w.gitTimer != nil {
		return
	}
	w.gitTimer = time.AfterFunc(w.window, func() {
		w.mu.Lock()
		w.gitTimer = nil
		f := w.OnGit
		closed := w.closed
		w.mu.Unlock()
		if f != nil && !closed {
			f()
		}
	})
}

// merge combines the pending kind with a new one within the window.
func merge(cur, next protocol.FileEventKind, fresh bool) protocol.FileEventKind {
	if fresh {
		return next
	}
	switch cur {
	case protocol.FileCreated:
		switch next {
		case protocol.FileDeleted:
			return ""
		default:
			return protocol.FileCreated
		}
	case protocol.FileModified:
		if next == protocol.FileDeleted {
			return protocol.FileDeleted
		}
		return protocol.FileModified
	case protocol.FileDeleted:
		if next == protocol.FileDeleted {
			return protocol.FileDeleted
		}
		return protocol.FileModified
	default: // created then deleted within the window
		if next == protocol.FileDeleted {
			return ""
		}
		return next
	}
}

func (w *Watcher) record(rel string, kind protocol.FileEventKind) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	p, ok := w.pending[rel]
	if ok {
		p.kind = merge(p.kind, kind, false)
		return
	}
	p = &pendingEvent{kind: kind}
	w.pending[rel] = p
	p.timer = time.AfterFunc(w.window, func() { w.fire(rel) })
}

func (w *Watcher) fire(rel string) {
	w.mu.Lock()
	p, ok := w.pending[rel]
	if ok {
		delete(w.pending, rel)
	}
	closed := w.closed
	w.mu.Unlock()
	if !ok || closed || p.kind == "" {
		return
	}
	w.emit(&protocol.FileEvent{Path: rel, Event: p.kind})
}
