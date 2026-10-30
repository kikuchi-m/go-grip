package internal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

const excludeTestHead = "ref: secret-head"

func excludeTestTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"a.md":       "# Visible\n",
		"secret.md":  "# Secret\n",
		"x.swp":      "swap",
		".git/HEAD":  excludeTestHead,
		"docs/b.md":  "# B\n",
		"docs/x.swp": "swap",
	})
}

func excludeServer(t *testing.T, reload bool, patterns ...string) *Server {
	t.Helper()

	ex, err := NewExcluder(patterns)
	if err != nil {
		t.Fatalf("NewExcluder: %v", err)
	}
	return NewServer("localhost", 6419, false, false, reload, NewParser(nil), nil, WithExcluder(ex))
}

func serveExcluded(t *testing.T, patterns []string, path string) *httptest.ResponseRecorder {
	t.Helper()

	server := excludeServer(t, false, patterns...)
	recorder := httptest.NewRecorder()
	server.newHandler(http.Dir(excludeTestTree(t))).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestExcludedPathsAreHiddenFromListing(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/", "/docs/"} {
		recorder := serveExcluded(t, []string{".git", "*.swp"}, path)

		body := recorder.Body.String()
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, recorder.Code)
		}
		for _, hidden := range []string{".git", "x.swp"} {
			if strings.Contains(body, hidden) {
				t.Errorf("GET %s: listing must hide %q, got %q", path, hidden, body)
			}
		}
	}
	if body := serveExcluded(t, []string{".git", "*.swp"}, "/").Body.String(); !strings.Contains(body, "a.md") {
		t.Errorf("listing must still show a.md, got %q", body)
	}
}

func TestWithoutExcludeEverythingIsListed(t *testing.T) {
	t.Parallel()

	server := NewServer("localhost", 6419, false, false, false, NewParser(nil), nil)
	recorder := httptest.NewRecorder()
	server.newHandler(http.Dir(excludeTestTree(t))).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	body := recorder.Body.String()
	for _, name := range []string{".git", "x.swp", "a.md"} {
		if !strings.Contains(body, name) {
			t.Errorf("without --exclude the listing must show %q, got %q", name, body)
		}
	}
}

func TestExcludedPathsReturnNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want int
	}{
		{"/.git/HEAD", http.StatusNotFound},
		{"/x.swp", http.StatusNotFound},
		{"/docs/x.swp", http.StatusNotFound},
		{"/a.md", http.StatusOK},
		{"/docs/b.md", http.StatusOK},
	}
	for _, tt := range tests {
		if got := serveExcluded(t, []string{".git", "*.swp"}, tt.path).Code; got != tt.want {
			t.Errorf("GET %s = %d, want %d", tt.path, got, tt.want)
		}
	}
}

func TestExcludedMarkdownIsNotRendered(t *testing.T) {
	t.Parallel()

	recorder := serveExcluded(t, []string{"secret.md"}, "/secret.md")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "Secret") {
		t.Fatalf("excluded markdown must not be rendered, got %q", recorder.Body.String())
	}
}

func TestExcludeIsNotBypassedByEncodedOrDotDotPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/%2Egit/HEAD", "/sub/../.git/HEAD", "/%2e%2e/.git/HEAD", "/.git%2FHEAD"} {
		recorder := serveExcluded(t, []string{".git"}, path)

		if recorder.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want the excluded file not to be served", path)
		}
		if strings.Contains(recorder.Body.String(), excludeTestHead) {
			t.Errorf("GET %s leaked excluded content: %q", path, recorder.Body.String())
		}
	}
}

func TestEventsRejectsExcludedPage(t *testing.T) {
	t.Parallel()

	hub, _ := runHub(t)
	server := excludeServer(t, true, "secret.md", ".git")
	server.hub = hub
	ts := httptest.NewServer(server.newHandler(http.Dir(excludeTestTree(t))))
	defer ts.Close()

	for path, want := range map[string]int{
		"/secret.md": http.StatusBadRequest,
		"/.git":      http.StatusBadRequest,
		// With an excluder set, other pages must still get a stream: the
		// exclude wrapper must not hide the root folder from live reload.
		"/a.md": http.StatusOK,
	} {
		resp, err := ts.Client().Get(ts.URL + "/__grip/events?path=" + path)
		if err != nil {
			t.Fatalf("get events for %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("events for %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}

func TestHubIgnoresExcludedNames(t *testing.T) {
	t.Parallel()

	ex, err := NewExcluder([]string{"*.log", ".git"})
	if err != nil {
		t.Fatalf("NewExcluder: %v", err)
	}
	hub, src := runHub(t)
	hub.exclude = ex

	notify, cancel := hub.Subscribe(WatchSet{Dirs: []string{osPath("r")}})
	defer cancel()

	src.events <- fsnotify.Event{Name: osPath("r", "build.log"), Op: fsnotify.Create}
	src.events <- fsnotify.Event{Name: osPath("r", ".git"), Op: fsnotify.Create}
	expectNoNotify(t, notify)

	src.events <- fsnotify.Event{Name: osPath("r", "new.md"), Op: fsnotify.Create}
	expectNotify(t, notify)
}
