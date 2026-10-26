package internal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectoryListingIgnoresCacheValidators(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	server := mustNewServer(t, Options{Host: "localhost", Port: 6419})
	handler := server.newHandler(http.Dir(tmpDir))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-Modified-Since", time.Now().Add(24*time.Hour).UTC().Format(http.TimeFormat))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected Cache-Control to disable storage, got %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "README.md") {
		t.Fatalf("expected directory listing body to mention README.md, got %q", recorder.Body.String())
	}
}

func TestRegularFileStillSupportsConditionalRequests(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "plain.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write plain.txt: %v", err)
	}

	server := mustNewServer(t, Options{Host: "localhost", Port: 6419})
	handler := server.newHandler(http.Dir(tmpDir))

	req := httptest.NewRequest(http.MethodGet, "/plain.txt", nil)
	req.Header.Set("If-Modified-Since", time.Now().Add(24*time.Hour).UTC().Format(http.TimeFormat))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotModified {
		t.Fatalf("expected status %d, got %d", http.StatusNotModified, recorder.Code)
	}
}

func TestMarkdownResponsesDisableCaching(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	server := mustNewServer(t, Options{Host: "localhost", Port: 6419})
	handler := server.newHandler(http.Dir(tmpDir))

	req := httptest.NewRequest(http.MethodGet, "/README.md", nil)
	req.Header.Set("If-Modified-Since", time.Now().Add(24*time.Hour).UTC().Format(http.TimeFormat))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected Cache-Control to disable storage, got %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/html" {
		t.Fatalf("expected text/html response, got %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "Hello") {
		t.Fatalf("expected rendered markdown response to contain document content, got %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "<title>README</title>") {
		t.Fatalf("expected filename-based HTML title, got %q", recorder.Body.String())
	}
}

func TestFormatFilenameTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{name: "humanizes separators", filename: "my-guide_v2.md", want: "My Guide V2"},
		{name: "uses basename", filename: "/docs/getting-started.md", want: "Getting Started"},
		{name: "preserves acronym", filename: "README.md", want: "README"},
		{name: "strips uppercase extension", filename: "release_notes.MD", want: "Release Notes"},
		{name: "collapses separators and whitespace", filename: "  release---notes__v2.md", want: "Release Notes V2"},
		{name: "supports unicode", filename: "überblick-plan.md", want: "Überblick Plan"},
		{name: "falls back for empty stem", filename: ".md", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatFilenameTitle(tt.filename); got != tt.want {
				t.Fatalf("formatFilenameTitle(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestFilenameTitleResponses(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	files := []string{"my-guide_v2.md", "unsafe-<script>.md", ".md"}
	for _, filename := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, filename), []byte("# Hello\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
	}

	server := mustNewServer(t, Options{Host: "localhost", Port: 6419})
	handler := server.newHandler(http.Dir(tmpDir))

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "humanized title", path: "/my-guide_v2.md", want: "<title>My Guide V2</title>"},
		{name: "escaped title", path: "/unsafe-%3Cscript%3E.md", want: "<title>Unsafe &lt;script&gt;</title>"},
		{name: "empty title fallback", path: "/.md", want: "<title>" + defaultHTMLTitle + "</title>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), tt.want) {
				t.Fatalf("expected response to contain %q, got %q", tt.want, recorder.Body.String())
			}
		})
	}
}

func mustNewServer(t *testing.T, opts Options) *Server {
	t.Helper()

	server, err := NewServer(opts, NewParser())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server
}

func newExcludeServerDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	files := map[string]string{
		"a.md":      "# Visible\n",
		"secret.md": "# Secret\n",
		"x.swp":     "swap",
		".git/HEAD": "ref: secret-head",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func serveExcluded(t *testing.T, exclude []string, path string) *httptest.ResponseRecorder {
	t.Helper()

	dir := newExcludeServerDir(t)
	server := mustNewServer(t, Options{Host: "localhost", Port: 6419, Exclude: exclude})
	handler := server.newHandler(server.fileSystem(dir))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestExcludedPathsAreHiddenFromListing(t *testing.T) {
	t.Parallel()

	recorder := serveExcluded(t, []string{".git", "*.swp"}, "/")

	body := recorder.Body.String()
	if !strings.Contains(body, "a.md") {
		t.Errorf("expected listing to contain a.md, got %q", body)
	}
	for _, hidden := range []string{".git", "x.swp"} {
		if strings.Contains(body, hidden) {
			t.Errorf("expected listing to hide %q, got %q", hidden, body)
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
		{"/a.md", http.StatusOK},
	}
	for _, tt := range tests {
		recorder := serveExcluded(t, []string{".git", "*.swp"}, tt.path)
		if recorder.Code != tt.want {
			t.Errorf("GET %s = %d, want %d", tt.path, recorder.Code, tt.want)
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

	for _, path := range []string{"/%2Egit/HEAD", "/sub/../.git/HEAD", "/%2e%2e/.git/HEAD"} {
		recorder := serveExcluded(t, []string{".git"}, path)

		if recorder.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want the excluded file not to be served", path)
		}
		if strings.Contains(recorder.Body.String(), "secret-head") {
			t.Errorf("GET %s leaked excluded content: %q", path, recorder.Body.String())
		}
	}
}

func TestNewServerRejectsInvalidExclude(t *testing.T) {
	t.Parallel()

	if _, err := NewServer(Options{Exclude: []string{"a/b"}}, NewParser()); err == nil {
		t.Fatal("expected an error for an exclude pattern containing '/'")
	}
}
