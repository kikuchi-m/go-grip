package internal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func getPath(t *testing.T, server *Server, dir, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	server.newHandler(http.Dir(dir)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// http.FileServer redirects /index.html to ./, which is a folder page here,
// so the file could never be viewed.
func TestIndexHTMLIsServedAsAFile(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"site/index.html": "<h1>hello</h1>", "index.html": "<h1>root</h1>"})
	server := NewServer("localhost", 6419, false, false, false, NewParser(nil), nil)

	for path, want := range map[string]string{"/site/index.html": "<h1>hello</h1>", "/index.html": "<h1>root</h1>"} {
		rec := getPath(t, server, dir, path)
		require.Equal(t, http.StatusOK, rec.Code, path)
		require.Equal(t, want, rec.Body.String(), path)
		require.Contains(t, rec.Header().Get("Content-Type"), "text/html", path)
	}
}

func TestDirWithIndexHTMLStillShowsFolderPage(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"site/index.html": "<h1>hello</h1>"})
	server := NewServer("localhost", 6419, false, false, false, NewParser(nil), nil)

	rec := getPath(t, server, dir, "/site/")

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEqual(t, "<h1>hello</h1>", rec.Body.String())
	require.Contains(t, rec.Body.String(), "index.html")
}

func TestExcludedIndexHTMLIsNotServed(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"site/index.html": "<h1>hello</h1>"})
	server := excludeServer(t, false, "index.html")

	require.Equal(t, http.StatusNotFound, getPath(t, server, dir, "/site/index.html").Code)
}
