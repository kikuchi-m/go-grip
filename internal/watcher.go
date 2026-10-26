package internal

import (
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 100 * time.Millisecond

// watcher watches a directory tree and tells the hub which pages are affected
// by a change: a markdown file that was written, or a directory whose entries
// were created, removed or renamed.
type watcher struct {
	root string
	ex   *Excluder
	hub  *reloadHub
	fsw  *fsnotify.Watcher

	mu     sync.Mutex
	timers map[string]*time.Timer
}

func newWatcher(root string, ex *Excluder, hub *reloadHub) (*watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{root: abs, ex: ex, hub: hub, fsw: fsw, timers: make(map[string]*time.Timer)}
	if err := w.addTree(abs); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

func (w *watcher) Close() error {
	err := w.fsw.Close()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, t := range w.timers {
		t.Stop()
	}
	return err
}

// addTree watches dir and every directory below it, except excluded ones.
func (w *watcher) addTree(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			// A directory that vanished while walking is not worth failing for.
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if rel, ok := w.rel(p); ok && w.ex.MatchPath(rel) {
			return filepath.SkipDir
		}
		if err := w.fsw.Add(p); err != nil {
			log.Printf("watcher: cannot watch %s: %v", p, err)
		}
		return nil
	})
}

// unwatchTree drops the watches at and below p. A renamed directory keeps its
// inotify watch under the old name, and registering the new name races
// fsnotify's own cleanup of the old one, which can then drop the new watch.
func (w *watcher) unwatchTree(p string) {
	for _, watched := range w.fsw.WatchList() {
		if watched == p || strings.HasPrefix(watched, p+string(filepath.Separator)) {
			_ = w.fsw.Remove(watched)
		}
	}
}

// rel returns p relative to the root as a slash path ("." for the root itself).
func (w *watcher) rel(p string) (string, bool) {
	r, err := filepath.Rel(w.root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(r), true
}

func (w *watcher) loop() {
	for {
		select {
		case e, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(e)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watcher: %v", err)
		}
	}
}

func (w *watcher) handle(e fsnotify.Event) {
	rel, ok := w.rel(e.Name)
	if !ok || rel == "." || w.ex.MatchPath(rel) {
		return
	}
	target := "/" + rel

	if e.Has(fsnotify.Remove) || e.Has(fsnotify.Rename) {
		w.unwatchTree(e.Name)
	}
	if e.Has(fsnotify.Create) {
		if info, err := os.Stat(e.Name); err == nil && info.IsDir() {
			if err := w.addTree(e.Name); err != nil {
				log.Printf("watcher: cannot watch %s: %v", e.Name, err)
			}
		}
	}
	if e.Has(fsnotify.Create) || e.Has(fsnotify.Remove) || e.Has(fsnotify.Rename) {
		w.schedule(path.Dir(target))
	}
	if (e.Has(fsnotify.Write) || e.Has(fsnotify.Create)) && strings.EqualFold(path.Ext(target), ".md") {
		w.schedule(target)
	}
}

// schedule merges bursts of changes to one target into a single notification.
func (w *watcher) schedule(target string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if old, ok := w.timers[target]; ok {
		old.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(debounceInterval, func() {
		w.mu.Lock()
		if w.timers[target] == t {
			delete(w.timers, target)
		}
		w.mu.Unlock()
		w.hub.notify(target)
	})
	w.timers[target] = t
}
