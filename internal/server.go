package internal

import (
	"bytes"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"

	chroma_html "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/chrishrb/go-grip/defaults"
)

const defaultHTMLTitle = "go-grip - markdown preview"

type Server struct {
	parser   *Parser
	opts     Options
	excluder *Excluder
}

func NewServer(opts Options, parser *Parser) (*Server, error) {
	excluder, err := NewExcluder(opts.Exclude)
	if err != nil {
		return nil, err
	}
	return &Server{
		parser:   parser,
		opts:     opts,
		excluder: excluder,
	}, nil
}

// fileSystem serves root without the entries matching the exclude patterns.
func (s *Server) fileSystem(root string) http.FileSystem {
	return newExcludeFS(http.Dir(root), s.excluder)
}

func (s *Server) Serve(file string) error {
	directory := path.Dir(file)
	filename := path.Base(file)

	handler, closeFn, err := s.buildHandler(directory)
	if err != nil {
		return err
	}
	defer closeFn()

	fsys := s.fileSystem(directory)

	addr := fmt.Sprintf("http://%s:%d/", s.opts.Host, s.opts.Port)
	if file == "" {
		// If README.md exists then open README.md at beginning
		readme := "README.md"
		f, err := fsys.Open(readme)
		if err == nil {
			//nolint:errcheck
			defer f.Close()
		}
		if err == nil {
			addr, _ = url.JoinPath(addr, readme)
		}
	} else {
		addr, _ = url.JoinPath(addr, filename)
	}

	fmt.Printf("🚀 Starting server: %s\n", addr)

	if s.opts.Browser {
		err := Open(addr)
		if err != nil {
			fmt.Println("❌ Error opening browser:", err)
		}
	}

	if s.opts.Reload {
		fmt.Printf("📡 Auto-reload enabled. Files will trigger browser refresh.\n")
	} else {
		fmt.Printf("🔄 Auto-reload disabled. Use F5 to manually refresh.\n")
	}
	return http.ListenAndServe(fmt.Sprintf(":%d", s.opts.Port), handler)
}

// buildHandler returns the handler serving directory and a function releasing
// what it holds (the file watcher). With reload enabled, a watcher notifies
// the pages whose files changed and every HTML response carries the script
// that listens for it.
func (s *Server) buildHandler(directory string) (http.Handler, func(), error) {
	handler := s.newHandler(s.fileSystem(directory))
	if !s.opts.Reload {
		return handler, func() {}, nil
	}

	hub := newReloadHub()
	wt, err := newWatcher(directory, s.excluder, hub)
	if err != nil {
		return nil, nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(reloadEndpoint, func(w http.ResponseWriter, r *http.Request) {
		serveReloadWS(hub, w, r)
	})
	mux.Handle("/", injectReloadScript(handler))
	return mux, func() {
		//nolint:errcheck
		wt.Close()
	}, nil
}

func (s *Server) newHandler(dir http.FileSystem) http.Handler {
	fileServer := http.FileServer(dir)
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(defaults.StaticFiles)))

	regex := regexp.MustCompile(`(?i)\.md$`)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if regex.MatchString(r.URL.Path) {
			isFile, err := isRegularFile(dir, r.URL.Path)
			if err == nil && isFile {
				setNoCacheHeaders(w)

				bytes, err := readToString(dir, r.URL.Path)
				if err != nil {
					log.Fatal(err)
					return
				}
				htmlContent, err := s.parser.MdToHTML(bytes)
				if err != nil {
					log.Fatal(err)
					return
				}

				err = serveTemplate(w, htmlStruct{
					Content:      string(htmlContent),
					BoundingBox:  s.opts.BoundingBox,
					CssCodeLight: getCssCode("github"),
					CssCodeDark:  getCssCode("github-dark"),
					Title:        html.EscapeString(s.pageTitle(r.URL.Path)),
				})
				if err != nil {
					log.Fatal(err)
					return
				}
				return
			}
		}

		isDirectory, err := isDirectory(dir, r.URL.Path)
		if err == nil && isDirectory {
			setNoCacheHeaders(w)
			stripCacheValidators(r)
		}

		fileServer.ServeHTTP(w, r)
	})

	return mux
}

func readToString(dir http.FileSystem, filename string) ([]byte, error) {
	f, err := dir.Open(filename)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck
	defer f.Close()

	var buf bytes.Buffer
	_, err = buf.ReadFrom(f)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type htmlStruct struct {
	Content      string
	BoundingBox  bool
	CssCodeLight string
	CssCodeDark  string
	Title        string
}

func (s *Server) pageTitle(filename string) string {
	title := formatFilenameTitle(filename)
	if title == "" {
		return defaultHTMLTitle
	}
	return title
}

func formatFilenameTitle(filename string) string {
	filename = path.Base(filename)
	extension := path.Ext(filename)
	if strings.EqualFold(extension, ".md") {
		filename = strings.TrimSuffix(filename, extension)
	}

	filename = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return ' '
		}
		return r
	}, filename)

	words := strings.Fields(filename)
	for i, word := range words {
		first, size := utf8.DecodeRuneInString(word)
		words[i] = string(unicode.ToUpper(first)) + word[size:]
	}
	return strings.Join(words, " ")
}

func serveTemplate(w http.ResponseWriter, html htmlStruct) error {
	w.Header().Set("Content-Type", "text/html")
	tmpl, err := template.ParseFS(defaults.Templates, "templates/layout.html")
	if err != nil {
		return err
	}
	err = tmpl.Execute(w, html)
	return err
}

func getCssCode(style string) string {
	buf := new(strings.Builder)
	formatter := chroma_html.New(chroma_html.WithClasses(true))
	s := styles.Get(style)
	_ = formatter.WriteCSS(buf, s)
	return buf.String()
}

func setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func stripCacheValidators(r *http.Request) {
	r.Header.Del("If-Modified-Since")
	r.Header.Del("If-None-Match")
}

func isDirectory(dir http.FileSystem, name string) (bool, error) {
	file, err := dir.Open(name)
	if err != nil {
		return false, err
	}
	//nolint:errcheck
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, err
	}

	return info.IsDir(), nil
}

func isRegularFile(dir http.FileSystem, name string) (bool, error) {
	file, err := dir.Open(name)
	if err != nil {
		return false, err
	}
	//nolint:errcheck
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, err
	}

	return !info.IsDir(), nil
}
