package internal

import (
	"net/http"

	"github.com/gorilla/websocket"
)

const reloadEndpoint = "/reload_ws"

var reloadUpgrader = websocket.Upgrader{
	// Same as before: avoid WebSocket CORS issues for development.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// serveReloadWS keeps a WebSocket open until the page's target changes or the
// browser goes away. The page reports its URL path in the "path" query.
func serveReloadWS(hub *reloadHub, w http.ResponseWriter, r *http.Request) {
	target, err := normalizeTarget(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Subscribe before upgrading so a change during the handshake is not lost.
	sub := hub.subscribe(target)
	defer hub.unsubscribe(sub)

	conn, err := reloadUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	//nolint:errcheck
	defer conn.Close()

	// Nothing is expected from the browser, but reading is how a closed tab is noticed.
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
	case <-sub.C:
		//nolint:errcheck
		conn.WriteMessage(websocket.TextMessage, []byte("reload"))
	case <-gone:
	}
}
