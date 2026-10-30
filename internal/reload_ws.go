package internal

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// wsPath is the live reload WebSocket endpoint, the counterpart of eventsPath.
//
// A browser keeps only a few long-lived HTTP connections per host open (six
// in Chrome), shared by every tab. One event stream per open page uses them
// up, and the next page load then waits for a free connection. An established
// WebSocket does not count against that limit.
const wsPath = "/__grip/ws"

// wsWriteWait bounds a write to a browser that has stopped reading.
const wsWriteWait = 5 * time.Second

// wsUpgrader keeps the default same-origin check: a page of another site
// must not be able to watch this server's files.
var wsUpgrader = websocket.Upgrader{}

// serveWS keeps a WebSocket open for one page and sends "reload" once when
// something that page shows has changed. Like serveEvents, the page names
// itself in the path query parameter.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request, root *Root, absRoot string) {
	page := r.URL.Query().Get("path")
	ws, ok := s.watchSetFor(root, absRoot, page)
	if !ok {
		s.log.Warn("live reload request rejected", "page", page)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ws.Page = page

	// Subscribe before upgrading so a change during the handshake is not lost.
	notify, cancel := s.hub.Subscribe(ws)
	defer cancel()

	// The request logger wraps the writer, and the wrapper cannot be hijacked.
	for {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		w = u.Unwrap()
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already replied with an error status.
		s.log.Debug("ws upgrade failed", "page", page, "err", err)
		return
	}
	//nolint:errcheck
	defer conn.Close()

	connected := time.Now()
	debug := s.debugging(r.Context())
	if debug {
		s.log.Debug("ws connect", "page", page)
		defer func() {
			s.log.Debug("ws disconnect", "page", page, "dur", time.Since(connected))
		}()
	}

	// Nothing is expected from the browser. Reading is how a closed tab is
	// noticed, so the watch and the connection are released right away.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-notify:
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		if err := conn.WriteMessage(websocket.TextMessage, []byte("reload")); err != nil {
			s.log.Debug("ws reload write failed", "page", page, "err", err)
			return
		}
		if debug {
			s.log.Debug("ws reload sent", "page", page)
		}
	case <-gone:
	}
}
