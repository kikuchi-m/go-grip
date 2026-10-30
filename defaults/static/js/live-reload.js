// Live reload: go-grip sends a "reload" message over a WebSocket when
// something this page shows has changed (the Markdown file, an image it
// references, or for a folder page, the folder's entries).
//
// A WebSocket is used instead of an event stream because a browser keeps only
// a few long-lived HTTP connections per host open (six in Chrome), shared by
// all tabs. One stream per open page uses them up and stalls the next page
// load; an established WebSocket does not count against that limit.
(function () {
  if (!window.WebSocket) return;

  var pagePath = location.pathname;
  try {
    pagePath = decodeURIComponent(pagePath);
  } catch (e) {}
  var url =
    (location.protocol === "https:" ? "wss://" : "ws://") +
    location.host +
    "/__grip/ws?path=" +
    encodeURIComponent(pagePath);

  var socket = null;
  var retryTimer = null;
  var everOpened = false;
  var lostConnection = false;
  var leaving = false;

  function connect() {
    var ws = new WebSocket(url);
    socket = ws;

    ws.onopen = function () {
      everOpened = true;
      // The connection came back after a drop: the server probably
      // restarted, so the page may be stale.
      if (lostConnection) {
        location.reload();
      }
    };

    ws.onmessage = function (event) {
      if (event.data === "reload") {
        leaving = true;
        location.reload();
      }
    };

    ws.onclose = function () {
      if (leaving || socket !== ws) return;
      // Never connecting means the server refused this page: do not retry.
      if (!everOpened) return;
      lostConnection = true;
      retryTimer = setTimeout(connect, 1000);
    };
  }

  // Leaving the page. Chrome's back/forward cache freezes the page instead of
  // unloading it, and would keep the connection open, so release it. An
  // unload handler would make the page ineligible for the cache.
  window.addEventListener("pagehide", function () {
    leaving = true;
    clearTimeout(retryTimer);
    if (socket) {
      socket.close();
      socket = null;
    }
  });

  // Back from the cache. Files may have changed while the page was frozen
  // and the server cannot tell which ones, so load the page fresh. On
  // localhost that costs one render, and the new page opens its own connection.
  window.addEventListener("pageshow", function (event) {
    if (event.persisted) {
      location.reload();
    }
  });

  connect();
})();
