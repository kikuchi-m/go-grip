package internal

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newBuiltServer(t *testing.T, opts Options, dir string) *httptest.Server {
	t.Helper()

	server := mustNewServer(t, opts)
	handler, closeFn, err := server.buildHandler(dir)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		srv.Close()
		closeFn()
	})
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string, header http.Header) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", path, err)
	}
	return resp, string(body)
}

func reloadOptions() Options {
	return Options{Host: "localhost", Port: 6419, Reload: true}
}

func TestInjectAddsScriptToMarkdownAndListing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# A\n")
	srv := newBuiltServer(t, reloadOptions(), dir)

	for _, path := range []string{"/a.md", "/"} {
		_, body := get(t, srv, path, nil)
		for _, want := range []string{"/reload_ws", "location.pathname"} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: expected body to contain %q", path, want)
			}
		}
	}
}

func TestInjectKeepsHTMLContentLengthValid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.html"), "<html><body>hi</body></html>")
	srv := newBuiltServer(t, reloadOptions(), dir)

	resp, body := get(t, srv, "/x.html", nil)

	if !strings.Contains(body, "location.pathname") {
		t.Errorf("expected the script to be injected, got %q", body)
	}
	if resp.ContentLength != -1 && resp.ContentLength != int64(len(body)) {
		t.Errorf("Content-Length %d does not match body length %d", resp.ContentLength, len(body))
	}
}

func TestInjectSkipsNonHTMLAndConditionalResponses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plain.txt"), "hello\n")
	srv := newBuiltServer(t, reloadOptions(), dir)

	_, body := get(t, srv, "/plain.txt", nil)
	if body != "hello\n" {
		t.Errorf("plain.txt body = %q, want it unchanged", body)
	}

	future := http.Header{"If-Modified-Since": {time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat)}}
	resp, body := get(t, srv, "/plain.txt", future)
	if resp.StatusCode != http.StatusNotModified || body != "" {
		t.Errorf("conditional GET = %d %q, want 304 with an empty body", resp.StatusCode, body)
	}
}

func TestNoReloadDisablesEverything(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# A\n")
	opts := reloadOptions()
	opts.Reload = false
	srv := newBuiltServer(t, opts, dir)

	_, body := get(t, srv, "/a.md", nil)
	if strings.Contains(body, "/reload_ws") {
		t.Error("the script must not be injected when reload is disabled")
	}
	resp, _ := get(t, srv, "/reload_ws", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /reload_ws = %d, want 404", resp.StatusCode)
	}
}

func TestReloadEndpointIsNotSubjectToExclude(t *testing.T) {
	t.Parallel()

	opts := reloadOptions()
	opts.Exclude = []string{"reload_ws"}
	srv := newBuiltServer(t, opts, t.TempDir())

	resp, _ := get(t, srv, "/reload_ws", nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET /reload_ws = %d, want 400 from the websocket handler", resp.StatusCode)
	}
}

func TestPageScopedReloadEndToEnd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# A\n")
	writeFile(t, filepath.Join(dir, "b.md"), "# B\n")
	srv := newBuiltServer(t, reloadOptions(), dir)
	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")

	dial := func(page string) *websocket.Conn {
		conn, resp, err := websocket.DefaultDialer.Dial(wsBase+"/reload_ws?path="+url.QueryEscape(page), nil)
		if err != nil {
			t.Fatalf("dial %s: %v", page, err)
		}
		_ = resp.Body.Close()
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	a, b := dial("/a.md"), dial("/b.md")

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = a.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, msg, err := a.ReadMessage(); err != nil || string(msg) != "reload" {
		t.Fatalf("a.md page got %q, %v; want reload", msg, err)
	}
	_ = b.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	if _, msg, err := b.ReadMessage(); err == nil {
		t.Fatalf("b.md page must not be reloaded, got %q", msg)
	}
}
