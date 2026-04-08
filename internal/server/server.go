// Package server implements the HTTP handlers, fragment cache,
// rate limiter, and template rendering for the product search web UI.
package server

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"log"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mbow/go-xsearch/ranking"
	"github.com/mbow/go-xsearch/xsearch"
)

const (
	MaxQueryLen   = 200
	maxSelectBody = 256
)

// App holds application state and HTTP handlers.
type App struct {
	Engine      *xsearch.Engine
	Ranker      *ranking.Ranker
	TemplateDir string
	indexTmpl   *template.Template
	Cache       *FragmentCache
	bufPool     sync.Pool
	limiter     *RateLimiter
}

// New creates an App with the given engine, ranker, and cache size.
func New(eng *xsearch.Engine, ranker *ranking.Ranker, cacheSize int) *App {
	app := &App{
		Engine:      eng,
		Ranker:      ranker,
		TemplateDir: "templates",
		Cache:       NewFragmentCache(cacheSize),
		limiter:     NewRateLimiter(50, 10, 5*time.Minute),
	}
	app.bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
	return app
}

// LoadTemplates parses the HTML templates from the TemplateDir directory.
func (app *App) LoadTemplates() {
	app.indexTmpl = template.Must(template.New("index.html").ParseFiles(app.TemplateDir + "/index.html"))
}

// Routes registers all HTTP routes on the given mux.
func (app *App) Routes(mux *http.ServeMux) {
	mux.Handle("GET /", securityHeaders(http.HandlerFunc(app.HandleIndex)))
	mux.Handle("GET /search", securityHeaders(app.limiter.Wrap(http.HandlerFunc(app.HandleSearch))))
	mux.Handle("POST /select", securityHeaders(csrfCheck(app.limiter.Wrap(http.HandlerFunc(app.HandleSelect)))))
	mux.Handle("GET /static/", securityHeaders(http.StripPrefix("/static/", noDirectoryListing(http.Dir("static")))))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			host := r.Host
			if !strings.HasSuffix(origin, "://"+host) {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func noDirectoryListing(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || r.URL.Path == "" {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
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
		for !utf8.ValidString(query) {
			query = query[:len(query)-1]
		}
	}

	if cached, ok := app.Cache.Get(query); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached) //nolint:errcheck
		return
	}

	var opts []xsearch.SearchOption
	if app.Ranker != nil {
		opts = append(opts, xsearch.WithScoring(app.Ranker.ScoreView()))
	}
	results := app.Engine.Search(query, opts...)
	buf := app.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer app.bufPool.Put(buf)

	renderResultsFragment(buf, query, results, app.Engine.Get)
	rendered := bytes.Clone(buf.Bytes())
	app.Cache.Set(query, rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(rendered)))
	w.Header().Set("X-Cache", "MISS")
	w.Write(rendered) //nolint:errcheck
}

func renderResultsFragment(buf *bytes.Buffer, query string, results []xsearch.Result, lookup func(string) (xsearch.Item, bool)) {
	buf.WriteString(`<div id="results-inner" data-ghost="`)
	buf.WriteString(html.EscapeString(ghostSuffix(query, results, lookup)))
	buf.WriteString("\">\n")

	hasDirect := false
	for _, res := range results {
		if res.MatchType != xsearch.MatchDirect {
			continue
		}
		item, ok := lookup(res.ID)
		if !ok {
			continue
		}
		if !hasDirect {
			buf.WriteString("<div class=\"result-section\">Results</div>\n")
			hasDirect = true
		}
		writeResultItem(buf, res, item)
	}

	hasFallback := false
	for _, res := range results {
		if res.MatchType != xsearch.MatchFallback {
			continue
		}
		item, ok := lookup(res.ID)
		if !ok {
			continue
		}
		if !hasFallback {
			buf.WriteString("<div class=\"result-section\">Related products</div>\n")
			hasFallback = true
		}
		writeResultItem(buf, res, item)
	}

	if !hasDirect && !hasFallback && query != "" {
		buf.WriteString("<div class=\"result-section\">No results found</div>\n")
	}

	buf.WriteString("</div>\n")
}

func writeResultItem(buf *bytes.Buffer, res xsearch.Result, item xsearch.Item) {
	_, nameHTML := displayName(item, res.Highlights)
	category := firstFieldValue(item, "category")

	buf.WriteString("<div class=\"result-item\"\n")
	buf.WriteString("     hx-post=\"/select\"\n")
	buf.WriteString("     hx-vals='{\"id\": \"")
	buf.WriteString(html.EscapeString(res.ID))
	buf.WriteString("\"}'\n")
	buf.WriteString("     hx-swap=\"none\"\n")
	buf.WriteString("     hx-indicator=\"false\">\n")
	buf.WriteString("    <div class=\"result-name\">")
	buf.WriteString(string(nameHTML))
	buf.WriteString("</div>\n")
	buf.WriteString("    <div class=\"result-category\">")
	buf.WriteString(html.EscapeString(category))
	buf.WriteString("</div>\n")
	buf.WriteString("</div>\n")
}

func firstFieldValue(item xsearch.Item, name string) string {
	for _, field := range item.Fields {
		if field.Name == name && len(field.Values) > 0 {
			return field.Values[0]
		}
	}
	if len(item.Fields) > 0 && len(item.Fields[0].Values) > 0 {
		return item.Fields[0].Values[0]
	}
	return ""
}

func displayName(item xsearch.Item, highlights map[string][]xsearch.Highlight) (string, template.HTML) {
	for _, field := range item.Fields {
		if field.Name != "name" || len(field.Values) == 0 {
			continue
		}
		valueIndex := 0
		if hs := highlights["name"]; len(hs) > 0 {
			valueIndex = hs[0].ValueIndex
		}
		if valueIndex < 0 || valueIndex >= len(field.Values) {
			valueIndex = 0
		}
		return field.Values[valueIndex], renderHighlightedValue(field.Values[valueIndex], highlights["name"], valueIndex)
	}
	for _, field := range item.Fields {
		if len(field.Values) == 0 {
			continue
		}
		valueIndex := 0
		if hs := highlights[field.Name]; len(hs) > 0 {
			valueIndex = hs[0].ValueIndex
		}
		if valueIndex < 0 || valueIndex >= len(field.Values) {
			valueIndex = 0
		}
		return field.Values[valueIndex], renderHighlightedValue(field.Values[valueIndex], highlights[field.Name], valueIndex)
	}
	return "", ""
}

func renderHighlightedValue(value string, highlights []xsearch.Highlight, valueIndex int) template.HTML {
	if len(highlights) == 0 {
		return template.HTML(html.EscapeString(value))
	}

	filtered := make([]xsearch.Highlight, 0, len(highlights))
	for _, h := range highlights {
		if h.ValueIndex == valueIndex {
			filtered = append(filtered, xsearch.Highlight{Start: h.Start, End: h.End})
		}
	}
	if len(filtered) == 0 {
		return template.HTML(html.EscapeString(value))
	}

	var b strings.Builder
	prev := 0
	for _, h := range filtered {
		if h.Start > prev {
			b.WriteString(html.EscapeString(value[prev:h.Start]))
		}
		b.WriteString("<mark>")
		b.WriteString(html.EscapeString(value[h.Start:h.End]))
		b.WriteString("</mark>")
		prev = h.End
	}
	if prev < len(value) {
		b.WriteString(html.EscapeString(value[prev:]))
	}
	return template.HTML(b.String())
}

func ghostSuffix(query string, results []xsearch.Result, lookup func(string) (xsearch.Item, bool)) string {
	if len(results) == 0 {
		return ""
	}
	item, ok := lookup(results[0].ID)
	if !ok {
		return ""
	}
	name, _ := displayName(item, results[0].Highlights)
	if len(query) > len(name) {
		return ""
	}
	if hasFoldedASCIIPrefix(name, query) {
		return name[len(query):]
	}
	if containsNonASCII(query) || containsNonASCII(name[:len(query)]) {
		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, query) {
			return name[len(query):]
		}
	}
	return ""
}

func hasFoldedASCIIPrefix(s, lowerPrefix string) bool {
	if len(lowerPrefix) > len(s) {
		return false
	}
	for i := range len(lowerPrefix) {
		sb := s[i]
		pb := lowerPrefix[i]
		if sb >= utf8.RuneSelf || pb >= utf8.RuneSelf {
			return false
		}
		if 'A' <= sb && sb <= 'Z' {
			sb += 'a' - 'A'
		}
		if sb != pb {
			return false
		}
	}
	return true
}

func containsNonASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return true
		}
	}
	return false
}

// HandleSelect records a user selecting a product and invalidates the cache.
func (app *App) HandleSelect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSelectBody)

	var id string
	if r.Header.Get("Content-Type") == "application/json" {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		id = req.ID
	} else {
		id = r.FormValue("id")
	}

	if id == "" {
		http.Error(w, "invalid product ID", http.StatusBadRequest)
		return
	}
	if _, ok := app.Engine.Get(id); !ok {
		http.Error(w, "product ID not found", http.StatusBadRequest)
		return
	}

	if app.Ranker != nil {
		app.Ranker.RecordSelection(id)
	}
	app.Cache.Invalidate()
	w.WriteHeader(http.StatusOK)
}

// FragmentCache is a cache for rendered HTML fragments.
type FragmentCache struct {
	mu      sync.RWMutex
	entries map[string][]byte
	maxSize int
}

func NewFragmentCache(maxSize int) *FragmentCache {
	return &FragmentCache{
		entries: make(map[string][]byte, maxSize),
		maxSize: maxSize,
	}
}

func (c *FragmentCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *FragmentCache) Set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxSize {
		evictCount := len(c.entries) / 2
		i := 0
		maps.DeleteFunc(c.entries, func(_ string, _ []byte) bool {
			if i >= evictCount {
				return false
			}
			i++
			return true
		})
	}
	c.entries[key] = value
}

func (c *FragmentCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

// RateLimiter implements a per-IP token bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	burst    int
	rate     float64
	ttl      time.Duration
}

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

func NewRateLimiter(burst int, rate float64, ttl time.Duration) *RateLimiter {
	return &RateLimiter{
		visitors: make(map[string]*visitor),
		burst:    burst,
		rate:     rate,
		ttl:      ttl,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[ip]
	if !ok {
		rl.visitors[ip] = &visitor{tokens: float64(rl.burst) - 1, lastSeen: now}
		if len(rl.visitors)%100 == 0 {
			rl.cleanupLocked(now)
		}
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.tokens = min(float64(rl.burst), v.tokens+elapsed*rl.rate)
	v.lastSeen = now
	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (rl *RateLimiter) cleanupLocked(now time.Time) {
	maps.DeleteFunc(rl.visitors, func(_ string, v *visitor) bool {
		return now.Sub(v.lastSeen) > rl.ttl
	})
}

func (rl *RateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.Allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}
