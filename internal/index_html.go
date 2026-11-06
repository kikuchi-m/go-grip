package internal

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
)

// serveFile writes the regular file at the request path, without the
// index.html redirect of http.FileServer. A missing file is answered with 404
// here, because the fallback would redirect to the folder. It reports false
// when the path cannot be handled (a folder, an unreadable file), and the
// caller falls back to http.FileServer.
func serveFile(w http.ResponseWriter, r *http.Request, fsys http.FileSystem) bool {
	f, err := fsys.Open(path.Clean("/" + r.URL.Path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return true
		}
		return false
	}
	//nolint:errcheck
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return true
}
