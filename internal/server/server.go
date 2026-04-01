// Package server implements the HTTP handlers, fragment cache,
// and template rendering for the product search web UI.
package server

import (
	"bytes"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
)

const (
	MaxQueryLen   = 200 // maximum search query length in bytes
	maxSelectBody = 256 // maximum POST /select body size in bytes
)

// App holds application state and HTTP handlers.
type App struct {
	Engine      *engine.Engine
	TemplateDir string // path to templates directory (default "templates")
	indexTmpl   *template.Template
	resultTmpl  *template.Template
	Cache       *FragmentCache
	bufPool     sync.Pool
}

// New creates an App with the given engine and cache size.
func New(eng *engine.Engine, cacheSize int) *App {
	app := &App{
		Engine:      eng,
		TemplateDir: "templates",
		Cache:       NewFragmentCache(cacheSize),
	}
	app.bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
	return app
}

// LoadTemplates parses the HTML templates from the TemplateDir directory.
// Must be called before serving requests.
func (app *App) LoadTemplates() {
	funcMap := template.FuncMap{
		"safe": func(s string) template.HTML { return template.HTML(s) },
	}
	app.indexTmpl = template.Must(template.New("index.html").Funcs(funcMap).ParseFiles(app.TemplateDir+"/index.html"))
	app.resultTmpl = template.Must(template.New("results.html").Funcs(funcMap).ParseFiles(app.TemplateDir+"/results.html"))
}

// Routes registers all HTTP routes on the given mux.
func (app *App) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", app.HandleIndex)
	mux.HandleFunc("GET /search", app.HandleSearch)
	mux.HandleFunc("POST /select", app.HandleSelect)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
}

// ResultsData is the template data for search results.
type ResultsData struct {
	Query           string
	DirectResults   []engine.Result
	FallbackResults []engine.Result
	Ghost           string
}

// HandleIndex serves the main search page.
func (app *App) HandleIndex(w http.ResponseWriter, r *http.Request) {
	if err := app.indexTmpl.Execute(w, nil); err != nil {
		log.Printf("error rendering index template: %v", err)
	}
}

// HandleSearch returns search results as an HTML fragment for HTMX.
func (app *App) HandleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	if len(query) > MaxQueryLen {
		query = query[:MaxQueryLen]
	}

	if cached, ok := app.Cache.Get(query); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached) //nolint:errcheck // best-effort write to HTTP client
		return
	}

	results := app.Engine.Search(query)

	data := ResultsData{Query: query}
	for _, res := range results {
		if res.MatchType == engine.MatchDirect {
			data.DirectResults = append(data.DirectResults, res)
		} else {
			data.FallbackResults = append(data.FallbackResults, res)
		}
	}

	// Ghost text: completion suffix from the top result.
	// Uses len(query) to index into name — safe because query is already
	// lowercased and ASCII lowering preserves byte length.
	if len(results) > 0 {
		name := results[0].Product.Name
		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, query) && len(query) <= len(name) {
			data.Ghost = name[len(query):]
		}
	}

	buf := app.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer app.bufPool.Put(buf)

	if err := app.resultTmpl.Execute(buf, data); err != nil {
		log.Printf("error rendering results template: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rendered := buf.Bytes()

	app.Cache.Set(query, rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(rendered)))
	w.Header().Set("X-Cache", "MISS")
	w.Write(rendered) //nolint:errcheck // best-effort write to HTTP client
}

// HandleSelect records a user selecting a product and invalidates the cache.
func (app *App) HandleSelect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSelectBody)

	var idStr string
	if r.Header.Get("Content-Type") == "application/json" {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		idStr = req.ID
	} else {
		idStr = r.FormValue("id")
	}

	id, err := strconv.Atoi(idStr)
	if err != nil || idStr == "" {
		http.Error(w, "invalid product ID", http.StatusBadRequest)
		return
	}

	productCount, _ := catalog.EmbeddedCount()
	if id < 0 || id >= productCount {
		http.Error(w, "product ID out of range", http.StatusBadRequest)
		return
	}

	app.Engine.RecordSelection(id)
	app.Cache.Invalidate()
	w.WriteHeader(http.StatusOK)
}

// FragmentCache is a simple cache for rendered HTML fragments.
// Keyed by query string, invalidated when selections change popularity.
type FragmentCache struct {
	mu      sync.RWMutex
	entries map[string][]byte
	maxSize int
}

// NewFragmentCache creates a new fragment cache with the given capacity.
func NewFragmentCache(maxSize int) *FragmentCache {
	return &FragmentCache{
		entries: make(map[string][]byte, maxSize),
		maxSize: maxSize,
	}
}

// Get returns the cached fragment for key, if present.
func (c *FragmentCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.entries[key]
	return v, ok
}

// Set stores a rendered fragment in the cache.
func (c *FragmentCache) Set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxSize {
		c.entries = make(map[string][]byte, c.maxSize)
	}
	c.entries[key] = value
}

// Invalidate clears all cached fragments.
func (c *FragmentCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string][]byte, c.maxSize)
}
