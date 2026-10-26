package internal

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNewExcluderRejectsInvalidPatterns(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"[", "a/b", "node_modules/", "/x"} {
		if _, err := NewExcluder([]string{p}); err == nil {
			t.Errorf("expected error for pattern %q", p)
		}
	}
	for _, patterns := range [][]string{{".git"}, {"*.swp"}, {}, nil} {
		if _, err := NewExcluder(patterns); err != nil {
			t.Errorf("unexpected error for %v: %v", patterns, err)
		}
	}
}

func TestExcluderMatchPath(t *testing.T) {
	t.Parallel()

	ex, err := NewExcluder([]string{".git", "*.swp"})
	if err != nil {
		t.Fatalf("NewExcluder: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"/.git/config", true},
		{"/a/b.swp", true},
		{"/a/b.md", false},
		{"/.gitignore", false},
		{"/", false},
	}
	for _, tt := range tests {
		if got := ex.MatchPath(tt.path); got != tt.want {
			t.Errorf("MatchPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}

	var nilEx *Excluder
	if nilEx.MatchPath("/.git/config") {
		t.Error("nil Excluder must match nothing")
	}
}

func newExcludeTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{".git/HEAD", "a.md", "x.swp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestExcludeFSOpenHidesExcluded(t *testing.T) {
	t.Parallel()

	ex, _ := NewExcluder([]string{".git", "*.swp"})
	fsys := newExcludeFS(http.Dir(newExcludeTestDir(t)), ex)

	for _, name := range []string{"/.git", "/.git/HEAD", "/x.swp"} {
		if _, err := fsys.Open(name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%q) error = %v, want ErrNotExist", name, err)
		}
	}
	f, err := fsys.Open("/a.md")
	if err != nil {
		t.Fatalf("Open(/a.md): %v", err)
	}
	_ = f.Close()
}

func TestExcludeFSDirectoryListingFiltered(t *testing.T) {
	t.Parallel()

	ex, _ := NewExcluder([]string{".git", "*.swp"})
	fsys := newExcludeFS(http.Dir(newExcludeTestDir(t)), ex)

	open := func() http.File {
		f, err := fsys.Open("/")
		if err != nil {
			t.Fatalf("Open(/): %v", err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}

	infos, err := open().Readdir(-1)
	if err != nil {
		t.Fatalf("Readdir(-1): %v", err)
	}
	var names []string
	for _, i := range infos {
		names = append(names, i.Name())
	}
	if len(names) != 1 || names[0] != "a.md" {
		t.Errorf("Readdir(-1) names = %v, want [a.md]", names)
	}

	rd, ok := open().(fs.ReadDirFile)
	if !ok {
		t.Fatal("wrapped directory must implement fs.ReadDirFile")
	}
	entries, err := rd.ReadDir(-1)
	if err != nil {
		t.Fatalf("ReadDir(-1): %v", err)
	}
	names = names[:0]
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) != 1 || names[0] != "a.md" {
		t.Errorf("ReadDir(-1) names = %v, want [a.md]", names)
	}

	f := open()
	got, err := f.Readdir(1)
	if err != nil || len(got) != 1 || got[0].Name() != "a.md" {
		t.Fatalf("Readdir(1) = %v, %v; want [a.md]", got, err)
	}
	if _, err := f.Readdir(1); err != io.EOF {
		t.Errorf("second Readdir(1) error = %v, want io.EOF", err)
	}
}
