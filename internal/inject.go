package internal

import (
	"fmt"
	"net/http"
	"strings"
)

// reloadScript connects the page to the reload endpoint, reporting which page
// it shows. When the connection drops it reconnects after a second and reloads
// the page once the server is back.
var reloadScript = fmt.Sprintf(`
<script>
	function retry() {
	  setTimeout(() => listen(true), 1000)
	}
	function listen(isRetry) {
	  let protocol = location.protocol === "https:" ? "wss://" : "ws://"
	  let ws = new WebSocket(protocol + location.host + "%s?path=" + encodeURIComponent(location.pathname))
	  if (isRetry) {
	    ws.onopen = () => window.location.reload()
	  }
	  ws.onmessage = function(msg) {
	    if (msg.data === "reload") {
	      window.location.reload()
	    }
	  }
	  ws.onclose = retry
	}
	listen(false)
</script>`, reloadEndpoint)

// injectReloadScript appends reloadScript to successful text/html responses.
func injectReloadScript(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		iw := &injectWriter{ResponseWriter: w}
		next.ServeHTTP(iw, r)
		iw.start(nil)
		if iw.inject {
			//nolint:errcheck
			iw.ResponseWriter.Write([]byte(reloadScript))
		}
	})
}

type injectWriter struct {
	http.ResponseWriter
	status  int
	started bool
	inject  bool
}

// start sends the header once the response is known to be HTML or not. A
// response without a Content-Type is sniffed from its first chunk, as net/http
// would.
func (w *injectWriter) start(firstChunk []byte) {
	if w.started {
		return
	}
	w.started = true
	code := w.status
	if code == 0 {
		code = http.StatusOK
	}
	if code == http.StatusOK {
		contentType := w.Header().Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(firstChunk)
		}
		w.inject = strings.HasPrefix(contentType, "text/html")
	}
	if w.inject {
		// The script makes the body longer than a declared Content-Length.
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *injectWriter) WriteHeader(code int) {
	if w.started || w.status != 0 {
		return
	}
	w.status = code
	// Without a Content-Type on a 200, the decision waits for the first chunk.
	if code != http.StatusOK || w.Header().Get("Content-Type") != "" {
		w.start(nil)
	}
}

func (w *injectWriter) Write(b []byte) (int, error) {
	w.start(b)
	return w.ResponseWriter.Write(b)
}
