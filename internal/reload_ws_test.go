package internal

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func dialGripWS(t *testing.T, ts *httptest.Server, page string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()

	base := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(base+"/__grip/ws?path="+url.QueryEscape(page), header)
	if err == nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, resp, err
}

// hubRefs returns how many folder watches the hub holds in total.
func hubRefs(h *Hub) int {
	h.watchMu.Lock()
	defer h.watchMu.Unlock()
	n := 0
	for _, c := range h.refs {
		n += c
	}
	return n
}

func TestWSSendsReloadOnlyToItsPage(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"docs/a.md": "# A\n", "docs/b.md": "# B\n"})
	server, src := reloadServer(t)
	ts := httptest.NewServer(server.newHandler(http.Dir(dir)))
	defer ts.Close()
	a, _, err := dialGripWS(t, ts, "/docs/a.md", nil)
	require.NoError(t, err)
	b, _, err := dialGripWS(t, ts, "/docs/b.md", nil)
	require.NoError(t, err)

	src.events <- fsnotify.Event{Name: filepath.Join(dir, "docs", "a.md"), Op: fsnotify.Write}

	_ = a.SetReadDeadline(time.Now().Add(2 * time.Second))
	typ, msg, err := a.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, typ)
	require.Equal(t, "reload", string(msg))
	// A timed out read breaks the connection, so b is only read once.
	_ = b.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, msg, err := b.ReadMessage(); err == nil {
		t.Fatalf("the other page must not be reloaded, got %q", msg)
	}
}

func TestWSReleasesWatchOnClientClose(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"docs/a.md": "# A\n"})
	server, src := reloadServer(t)
	ts := httptest.NewServer(server.newHandler(http.Dir(dir)))
	defer ts.Close()
	conn, _, err := dialGripWS(t, ts, "/docs/a.md", nil)
	require.NoError(t, err)

	_ = conn.Close()

	require.Eventually(t, func() bool {
		_, removed := src.calls()
		return len(removed) == 1 && removed[0] == filepath.Join(dir, "docs")
	}, 2*time.Second, 10*time.Millisecond)
}

func TestWSReleasesManyConnections(t *testing.T) {
	t.Parallel()

	const n = 30
	dir := writeTree(t, map[string]string{"docs/a.md": "# A\n"})
	server, _ := reloadServer(t)
	ts := httptest.NewServer(server.newHandler(http.Dir(dir)))
	defer ts.Close()
	conns := make([]*websocket.Conn, 0, n)
	for range n {
		conn, _, err := dialGripWS(t, ts, "/docs/a.md", nil)
		require.NoError(t, err)
		conns = append(conns, conn)
	}
	require.Eventually(t, func() bool { return hubRefs(server.hub) == n }, 5*time.Second, 10*time.Millisecond)

	for _, c := range conns {
		_ = c.Close()
	}

	require.Eventually(t, func() bool { return hubRefs(server.hub) == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestWSRejectsBadPages(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"docs/a.md": "# A\n"})
	server, _ := reloadServer(t)
	ts := httptest.NewServer(server.newHandler(http.Dir(dir)))
	defer ts.Close()

	for _, page := range []string{"", "x.md", "/nope.xyz", "/docs/../docs/a.md"} {
		_, resp, err := dialGripWS(t, ts, page, nil)
		if err == nil {
			t.Errorf("page %q: expected the handshake to fail", page)
			continue
		}
		if resp == nil || resp.StatusCode != http.StatusBadRequest {
			t.Errorf("page %q: response = %v, want status 400", page, resp)
		}
	}
}

func TestWSRejectsExcludedPage(t *testing.T) {
	t.Parallel()

	hub, _ := runHub(t)
	server := excludeServer(t, true, "secret.md", ".git")
	server.hub = hub
	ts := httptest.NewServer(server.newHandler(http.Dir(excludeTestTree(t))))
	defer ts.Close()

	for _, page := range []string{"/secret.md", "/.git"} {
		_, resp, err := dialGripWS(t, ts, page, nil)
		if err == nil || resp == nil || resp.StatusCode != http.StatusBadRequest {
			t.Errorf("page %q: err = %v, response = %v; want a 400 handshake failure", page, err, resp)
		}
	}
	if _, _, err := dialGripWS(t, ts, "/a.md", nil); err != nil {
		t.Errorf("an ordinary page must still connect: %v", err)
	}
}

func TestWSRejectsCrossOrigin(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"a.md": "# A\n"})
	server, _ := reloadServer(t)
	ts := httptest.NewServer(server.newHandler(http.Dir(dir)))
	defer ts.Close()

	_, resp, err := dialGripWS(t, ts, "/a.md", http.Header{"Origin": {"http://evil.example"}})

	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("err = %v, response = %v; want a 403 handshake failure", err, resp)
	}
}

// At debug level every request goes through a response recorder that does not
// implement http.Hijacker; the upgrade must still work.
func TestWSWorksWithDebugLogging(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{"a.md": "# A\n"})
	hub, _ := runHub(t)
	server := NewServer("localhost", 6419, false, false, true, NewParser(nil), NewLogger(io.Discard, slog.LevelDebug))
	server.hub = hub
	ts := httptest.NewServer(server.newHandler(http.Dir(dir)))
	defer ts.Close()

	if _, _, err := dialGripWS(t, ts, "/a.md", nil); err != nil {
		t.Fatalf("dial with debug logging on: %v", err)
	}
}

func TestLiveReloadScriptUsesWebSocket(t *testing.T) {
	t.Parallel()

	server, _ := reloadServer(t)
	recorder := httptest.NewRecorder()
	server.newHandler(http.Dir(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/js/live-reload.js", nil))

	body := recorder.Body.String()
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, body, "new WebSocket")
	require.Contains(t, body, "/__grip/ws")
	require.NotContains(t, body, "EventSource")
}
