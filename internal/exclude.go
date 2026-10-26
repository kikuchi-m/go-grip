package internal

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Excluder decides which file and directory names are hidden from the
// directory listing, direct access and the file watcher.
type Excluder struct {
	patterns []string
}

// NewExcluder validates the patterns. Each pattern is matched against a single
// name (never a path), so patterns containing "/" are rejected.
func NewExcluder(patterns []string) (*Excluder, error) {
	for _, p := range patterns {
		// MatchPath cleans the path first, so no segment is ever empty, "." or "..".
		if p == "" || p == "." || p == ".." {
			return nil, fmt.Errorf("invalid exclude pattern %q: it can never match a file or directory name", p)
		}
		if strings.Contains(p, "/") {
			msg := fmt.Sprintf("invalid exclude pattern %q: patterns match one file or directory name, so '/' is not allowed", p)
			if name := strings.Trim(p, "/"); name != "" && !strings.Contains(name, "/") {
				msg += fmt.Sprintf("; did you mean %q?", name)
			}
			return nil, errors.New(msg)
		}
		// path.Match validates the whole pattern even when the name does not match.
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
	}
	return &Excluder{patterns: append([]string(nil), patterns...)}, nil
}

// Match reports whether a single file or directory name is excluded.
func (e *Excluder) Match(name string) bool {
	if e == nil {
		return false
	}
	for _, p := range e.patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// MatchPath reports whether any segment of the slash-separated path is excluded.
func (e *Excluder) MatchPath(p string) bool {
	if e == nil {
		return false
	}
	for _, seg := range strings.Split(path.Clean("/"+p), "/") {
		if seg != "" && e.Match(seg) {
			return true
		}
	}
	return false
}

type excludeFS struct {
	fsys http.FileSystem
	ex   *Excluder
}

func newExcludeFS(fsys http.FileSystem, ex *Excluder) http.FileSystem {
	return excludeFS{fsys: fsys, ex: ex}
}

func (e excludeFS) Open(name string) (http.File, error) {
	if e.ex.MatchPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	f, err := e.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	return &excludeFile{File: f, ex: e.ex}, nil
}

// excludeFile drops excluded entries from directory reads. http.FileServer
// lists directories through Readdir or ReadDir depending on the Go version,
// so both are filtered.
type excludeFile struct {
	http.File
	ex *Excluder
}

func (f *excludeFile) Readdir(count int) ([]fs.FileInfo, error) {
	return readFiltered(count, f.File.Readdir, func(i fs.FileInfo) bool { return !f.ex.Match(i.Name()) })
}

func (f *excludeFile) ReadDir(n int) ([]fs.DirEntry, error) {
	read := func(n int) ([]fs.DirEntry, error) {
		if rd, ok := f.File.(fs.ReadDirFile); ok {
			return rd.ReadDir(n)
		}
		infos, err := f.File.Readdir(n)
		entries := make([]fs.DirEntry, len(infos))
		for i, info := range infos {
			entries[i] = fs.FileInfoToDirEntry(info)
		}
		return entries, err
	}
	return readFiltered(n, read, func(e fs.DirEntry) bool { return !f.ex.Match(e.Name()) })
}

// readFiltered follows the Readdir(count) contract: count <= 0 returns every
// remaining kept entry, count > 0 returns at most count kept entries and
// io.EOF once nothing is left.
func readFiltered[T any](count int, read func(int) ([]T, error), keep func(T) bool) ([]T, error) {
	var out []T
	for {
		want := count
		if count > 0 {
			want = count - len(out)
		}
		batch, err := read(want)
		for _, b := range batch {
			if keep(b) {
				out = append(out, b)
			}
		}
		if count <= 0 {
			return out, err
		}
		if err == nil && len(batch) == 0 {
			err = io.EOF
		}
		if err != nil {
			if err == io.EOF && len(out) > 0 {
				return out, nil
			}
			return out, err
		}
		if len(out) >= count {
			return out, nil
		}
	}
}
