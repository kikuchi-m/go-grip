package internal

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func newReloadWSServer(t *testing.T) (*reloadHub, string) {
	t.Helper()

	hub := newReloadHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveReloadWS(hub, w, r)
	}))
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dialReloadWS(t *testing.T, base, path string) *websocket.Conn {
	t.Helper()

	conn, resp, err := websocket.DefaultDialer.Dial(base+"/?path="+url.QueryEscape(path), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	_ = resp.Body.Close()
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestServeReloadWSSendsReloadForMatchingTarget(t *testing.T) {
	t.Parallel()

	hub, base := newReloadWSServer(t)
	conn := dialReloadWS(t, base, "/a.md")
	require.Eventually(t, func() bool { return hub.count() == 1 }, 2*time.Second, 10*time.Millisecond)

	hub.notify("/a.md")

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	typ, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if typ != websocket.TextMessage || string(msg) != "reload" {
		t.Fatalf("got message type %d %q, want text %q", typ, msg, "reload")
	}
}

func TestServeReloadWSIgnoresOtherTargets(t *testing.T) {
	t.Parallel()

	hub, base := newReloadWSServer(t)
	conn := dialReloadWS(t, base, "/a.md")
	require.Eventually(t, func() bool { return hub.count() == 1 }, 2*time.Second, 10*time.Millisecond)

	hub.notify("/b.md")

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, msg, err := conn.ReadMessage()
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected a read timeout, got message %q, error %v", msg, err)
	}
}

func TestServeReloadWSReleasesSubscriptionOnClientClose(t *testing.T) {
	t.Parallel()

	hub, base := newReloadWSServer(t)
	conn := dialReloadWS(t, base, "/a.md")
	require.Eventually(t, func() bool { return hub.count() == 1 }, 2*time.Second, 10*time.Millisecond)

	_ = conn.Close()

	require.Eventually(t, func() bool { return hub.count() == 0 }, 2*time.Second, 10*time.Millisecond)
}

func TestServeReloadWSReleasesManyConnections(t *testing.T) {
	t.Parallel()

	hub, base := newReloadWSServer(t)
	const n = 50
	conns := make([]*websocket.Conn, 0, n)
	for range n {
		conns = append(conns, dialReloadWS(t, base, "/a.md"))
	}
	require.Eventually(t, func() bool { return hub.count() == n }, 5*time.Second, 10*time.Millisecond)

	for _, c := range conns {
		_ = c.Close()
	}

	require.Eventually(t, func() bool { return hub.count() == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestServeReloadWSRejectsBadPath(t *testing.T) {
	t.Parallel()

	_, base := newReloadWSServer(t)

	for _, query := range []string{"", "?path=a.md"} {
		_, resp, err := websocket.DefaultDialer.Dial(base+"/"+query, nil)
		if err == nil {
			t.Fatalf("dial %q: expected the handshake to fail", query)
		}
		if resp == nil || resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("dial %q: response = %v, want status 400", query, resp)
		}
		_ = resp.Body.Close()
	}
}
