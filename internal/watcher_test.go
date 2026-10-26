package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitNotified(t *testing.T, sub *reloadSub) {
	t.Helper()

	select {
	case <-sub.C:
	case <-time.After(2 * time.Second):
		t.Fatalf("subscriber of %q was not notified", sub.target)
	}
}

func assertSilent(t *testing.T, sub *reloadSub) {
	t.Helper()

	select {
	case <-sub.C:
		t.Fatalf("subscriber of %q was notified unexpectedly", sub.target)
	case <-time.After(400 * time.Millisecond):
	}
}

func startTestWatcher(t *testing.T, root string, exclude ...string) *reloadHub {
	t.Helper()

	ex, err := NewExcluder(exclude)
	if err != nil {
		t.Fatalf("NewExcluder: %v", err)
	}
	hub := newReloadHub()
	w, err := newWatcher(root, ex, hub)
	if err != nil {
		t.Fatalf("newWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return hub
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func TestWatcherNotifiesOnMarkdownWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "old")
	hub := startTestWatcher(t, root)
	sub := hub.subscribe("/a.md")

	writeFile(t, filepath.Join(root, "a.md"), "new")

	waitNotified(t, sub)
}

func TestWatcherIgnoresOtherFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "a")
	writeFile(t, filepath.Join(root, "b.md"), "b")
	writeFile(t, filepath.Join(root, "x.txt"), "x")
	hub := startTestWatcher(t, root)
	page := hub.subscribe("/a.md")
	dir := hub.subscribe("/")

	writeFile(t, filepath.Join(root, "b.md"), "b2")
	writeFile(t, filepath.Join(root, "x.txt"), "x2")

	assertSilent(t, page)
	assertSilent(t, dir)
}

func TestWatcherNotifiesDirectoryOnChildCreateAndRemove(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mkdir(t, filepath.Join(root, "sub"))
	hub := startTestWatcher(t, root)
	rootSub := hub.subscribe("/")
	subSub := hub.subscribe("/sub")

	writeFile(t, filepath.Join(root, "n.txt"), "n")
	waitNotified(t, rootSub)
	if err := os.Remove(filepath.Join(root, "n.txt")); err != nil {
		t.Fatal(err)
	}
	waitNotified(t, rootSub)

	assertSilent(t, subSub)
	writeFile(t, filepath.Join(root, "sub", "f.txt"), "f")
	waitNotified(t, subSub)
}

func TestWatcherTreatsRenameSaveAsWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "old")
	hub := startTestWatcher(t, root)
	sub := hub.subscribe("/a.md")

	writeFile(t, filepath.Join(root, "a.md.tmp"), "new")
	if err := os.Rename(filepath.Join(root, "a.md.tmp"), filepath.Join(root, "a.md")); err != nil {
		t.Fatal(err)
	}

	waitNotified(t, sub)
}

func TestWatcherSkipsExcluded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mkdir(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, ".git", "x"), "x")
	hub := startTestWatcher(t, root, ".git", "*.swp")
	rootSub := hub.subscribe("/")
	gitSub := hub.subscribe("/.git")

	writeFile(t, filepath.Join(root, ".git", "x"), "x2")
	writeFile(t, filepath.Join(root, "a.swp"), "swap")

	assertSilent(t, rootSub)
	assertSilent(t, gitSub)
}

func TestWatcherWatchesNewSubdirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hub := startTestWatcher(t, root)
	rootSub := hub.subscribe("/")
	pageSub := hub.subscribe("/sub/b.md")

	mkdir(t, filepath.Join(root, "sub"))
	waitNotified(t, rootSub)
	writeFile(t, filepath.Join(root, "sub", "b.md"), "b")

	waitNotified(t, pageSub)
}

func TestWatcherSurvivesRemovedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mkdir(t, filepath.Join(root, "sub"))
	writeFile(t, filepath.Join(root, "sub", "f.txt"), "f")
	writeFile(t, filepath.Join(root, "a.md"), "old")
	hub := startTestWatcher(t, root)
	rootSub := hub.subscribe("/")
	sub := hub.subscribe("/a.md")

	if err := os.RemoveAll(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	waitNotified(t, rootSub)
	writeFile(t, filepath.Join(root, "a.md"), "new")

	waitNotified(t, sub)
}

// Renaming a watched directory races the re-registration of the new name
// against fsnotify's own handling of the old watch, so one directory is not
// enough to see it fail reliably.
func TestWatcherWatchesRenamedDirectories(t *testing.T) {
	t.Parallel()

	const n = 20
	root := t.TempDir()
	for i := range n {
		mkdir(t, filepath.Join(root, fmt.Sprintf("d%d", i)))
		writeFile(t, filepath.Join(root, fmt.Sprintf("d%d", i), "a.md"), "old")
	}
	hub := startTestWatcher(t, root)
	rootSub := hub.subscribe("/")
	subs := make([]*reloadSub, n)
	for i := range n {
		subs[i] = hub.subscribe(fmt.Sprintf("/e%d/a.md", i))
	}

	for i := range n {
		if err := os.Rename(filepath.Join(root, fmt.Sprintf("d%d", i)), filepath.Join(root, fmt.Sprintf("e%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	waitNotified(t, rootSub)
	for i := range n {
		writeFile(t, filepath.Join(root, fmt.Sprintf("e%d", i), "a.md"), "new")
	}

	for i := range n {
		waitNotified(t, subs[i])
	}
}

func TestWatcherForgetsDirectoryMovedOutOfTree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	mkdir(t, filepath.Join(root, "docs", "sub"))
	writeFile(t, filepath.Join(root, "docs", "sub", "x.md"), "old")
	hub := startTestWatcher(t, root)
	rootSub := hub.subscribe("/")
	stale := hub.subscribe("/docs/sub/x.md")

	if err := os.Rename(filepath.Join(root, "docs"), filepath.Join(outside, "docs")); err != nil {
		t.Fatal(err)
	}
	waitNotified(t, rootSub)
	writeFile(t, filepath.Join(outside, "docs", "sub", "x.md"), "new")

	assertSilent(t, stale)
}

func TestWatcherDebouncesBursts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "0")
	hub := startTestWatcher(t, root)
	sub := hub.subscribe("/a.md")

	for _, c := range []string{"1", "2", "3", "4", "5"} {
		writeFile(t, filepath.Join(root, "a.md"), c)
	}

	waitNotified(t, sub)
	assertSilent(t, sub)
}
