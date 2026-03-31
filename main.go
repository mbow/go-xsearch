package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
)

const (
	maxQueryLen    = 200        // maximum search query length in bytes
	maxSelectBody  = 256        // maximum POST /select body size in bytes
	snapshotPeriod = 60 * time.Second
)

// App holds application state.
type App struct {
	engine     *engine.Engine
	indexTmpl  *template.Template
	resultTmpl *template.Template
	dataDir    string
	cache      *fragmentCache
}

// ResultsData is the template data for search results.
type ResultsData struct {
	Query           string
	DirectResults   []engine.Result
	FallbackResults []engine.Result
}

// fragmentCache is a simple LRU cache for rendered HTML fragments.
// Keyed by query string, invalidated when selections change popularity.
type fragmentCache struct {
	mu      sync.RWMutex
	entries map[string][]byte
	maxSize int
}

func newFragmentCache(maxSize int) *fragmentCache {
	return &fragmentCache{
		entries: make(map[string][]byte, maxSize),
		maxSize: maxSize,
	}
}

func (c *fragmentCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *fragmentCache) set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Simple eviction: if at capacity, clear everything.
	// For a product catalog with debounced queries, this rarely triggers.
	if len(c.entries) >= c.maxSize {
		c.entries = make(map[string][]byte, c.maxSize)
	}
	c.entries[key] = value
}

func (c *fragmentCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string][]byte, c.maxSize)
}

func (app *App) loadTemplates() {
	app.indexTmpl = template.Must(template.ParseFiles("templates/index.html"))
	app.resultTmpl = template.Must(template.ParseFiles("templates/results.html"))
}

func (app *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := app.indexTmpl.Execute(w, nil); err != nil {
		log.Printf("error rendering index template: %v", err)
	}
}

func (app *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	// Truncate overly long queries to bound cache key size and search work.
	if len(query) > maxQueryLen {
		query = query[:maxQueryLen]
	}

	// Check fragment cache first.
	if cached, ok := app.cache.get(query); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached) //nolint:errcheck // best-effort write to HTTP client
		return
	}

	results := app.engine.Search(query)

	data := ResultsData{Query: query}
	for _, res := range results {
		if res.MatchType == engine.MatchDirect {
			data.DirectResults = append(data.DirectResults, res)
		} else {
			data.FallbackResults = append(data.FallbackResults, res)
		}
	}

	var buf bytes.Buffer
	if err := app.resultTmpl.Execute(&buf, data); err != nil {
		log.Printf("error rendering results template: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rendered := buf.Bytes()

	app.cache.set(query, rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(rendered)))
	w.Header().Set("X-Cache", "MISS")
	w.Write(rendered) //nolint:errcheck // best-effort write to HTTP client
}

func (app *App) handleSelect(w http.ResponseWriter, r *http.Request) {
	// Limit body size to prevent abuse.
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

	// Reject IDs outside the valid product range.
	productCount, _ := catalog.EmbeddedCount()
	if id < 0 || id >= productCount {
		http.Error(w, "product ID out of range", http.StatusBadRequest)
		return
	}

	app.engine.RecordSelection(id)
	app.cache.invalidate()
	w.WriteHeader(http.StatusOK)
}

func (app *App) startSnapshots(interval time.Duration) {
	path := filepath.Join(app.dataDir, "popularity.json")
	go func() {
		for range time.Tick(interval) {
			if err := app.engine.Ranker().Save(path); err != nil {
				log.Printf("error saving popularity data: %v", err)
			}
		}
	}()
}

func main() {
	dataDir := "data"

	// Load products + pre-built index from compiled-in CBOR data (no file I/O)
	products, err := catalog.EmbeddedProducts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded products: %v\n", err)
		os.Exit(1)
	}

	bloomRaw, err := catalog.EmbeddedBloomRaw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded bloom: %v\n", err)
		os.Exit(1)
	}

	indexRaw, err := catalog.EmbeddedIndexRaw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded index: %v\n", err)
		os.Exit(1)
	}

	bm25Raw, err := catalog.EmbeddedBM25Raw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded bm25: %v\n", err)
		os.Exit(1)
	}

	eng, err := engine.NewFromEmbedded(products, bloomRaw, indexRaw, bm25Raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating engine: %v\n", err)
		os.Exit(1)
	}

	app := &App{
		engine:  eng,
		dataDir: dataDir,
		cache:   newFragmentCache(1024),
	}
	app.loadTemplates()

	// Load popularity data if it exists
	popPath := filepath.Join(dataDir, "popularity.json")
	if err := app.engine.Ranker().Load(popPath); err != nil {
		log.Printf("warning: could not load popularity data: %v", err)
	}

	// Prune old data and start periodic snapshots
	app.engine.Ranker().Prune(90)
	app.startSnapshots(snapshotPeriod)

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("GET /search", app.handleSearch)
	mux.HandleFunc("POST /select", app.handleSelect)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	addr := ":8080"
	log.Printf("starting server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
