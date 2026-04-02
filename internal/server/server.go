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

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
)

const (
	MaxQueryLen   = 200 // maximum search query length in bytes
	maxSelectBody = 256 // maximum POST /select body size in bytes
)

// App holds application state and HTTP handlers.
type App struct {
	Engine       *engine.Engine
	TemplateDir  string // path to templates directory (default "templates")
	indexTmpl    *template.Template
	Cache        *FragmentCache
	bufPool      sync.Pool
	productCount int          // cached product count for validation
	limiter      *RateLimiter // per-IP rate limiter
}

// New creates an App with the given engine and cache size.
func New(eng *engine.Engine, cacheSize int) *App {
	productCount, _ := catalog.EmbeddedCount()
	app := &App{
		Engine:       eng,
		TemplateDir:  "templates",
		Cache:        NewFragmentCache(cacheSize),
		productCount: productCount,
		limiter:      NewRateLimiter(50, 10, 5*time.Minute), // 50 req/s burst, 10 req/s sustained
	}
	app.bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
	return app
}

// LoadTemplates parses the HTML templates from the TemplateDir directory.
// Must be called before serving requests.
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

// securityHeaders adds standard security response headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// csrfCheck rejects cross-origin POST requests by validating the Origin header.
func csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Origin present — verify it matches the Host.
			host := r.Host
			if !strings.HasSuffix(origin, "://"+host) {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		// No Origin header means same-origin or non-browser client — allow.
		// HTMX requests include Origin, so legitimate browser requests are covered.
		next.ServeHTTP(w, r)
	})
}

// noDirectoryListing wraps an http.FileSystem to return 404 for directory requests.
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
		// Ensure we don't split a multi-byte UTF-8 character.
		for !utf8.ValidString(query) {
			query = query[:len(query)-1]
		}
	}

	if cached, ok := app.Cache.Get(query); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(cached)))
		w.Header().Set("X-Cache", "HIT")
		w.Write(cached) //nolint:errcheck // best-effort write to HTTP client
		return
	}

	results := app.Engine.Search(query)

	buf := app.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer app.bufPool.Put(buf)

	renderResultsFragment(buf, query, results)
	rendered := bytes.Clone(buf.Bytes())

	app.Cache.Set(query, rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(rendered)))
	w.Header().Set("X-Cache", "MISS")
	w.Write(rendered) //nolint:errcheck // best-effort write to HTTP client
}

func renderResultsFragment(buf *bytes.Buffer, query string, results []engine.Result) {
	buf.WriteString(`<div id="results-inner" data-ghost="`)
	buf.WriteString(html.EscapeString(ghostSuffix(query, results)))
	buf.WriteString("\">\n")

	hasDirect := false
	for _, res := range results {
		if res.MatchType != engine.MatchDirect {
			continue
		}
		if !hasDirect {
			buf.WriteString("<div class=\"result-section\">Results</div>\n")
			hasDirect = true
		}
		writeResultItem(buf, res)
	}

	hasFallback := false
	for _, res := range results {
		if res.MatchType != engine.MatchFallback {
			continue
		}
		if !hasFallback {
			buf.WriteString("<div class=\"result-section\">Related products</div>\n")
			hasFallback = true
		}
		writeResultItem(buf, res)
	}

	if !hasDirect && !hasFallback && query != "" {
		buf.WriteString("<div class=\"result-section\">No results found</div>\n")
	}

	buf.WriteString("</div>\n")
}

func writeResultItem(buf *bytes.Buffer, res engine.Result) {
	buf.WriteString("<div class=\"result-item\"\n")
	buf.WriteString("     hx-post=\"/select\"\n")
	buf.WriteString("     hx-vals='{\"id\": \"")
	writeInt(buf, res.ProductID)
	buf.WriteString("\"}'\n")
	buf.WriteString("     hx-swap=\"none\"\n")
	buf.WriteString("     hx-indicator=\"false\">\n")
	buf.WriteString("    <div class=\"result-name\">")
	buf.WriteString(string(res.HighlightedName))
	buf.WriteString("</div>\n")
	buf.WriteString("    <div class=\"result-category\">")
	if res.Product != nil {
		buf.WriteString(html.EscapeString(res.Product.Category))
	}
	buf.WriteString("</div>\n")
	buf.WriteString("</div>\n")
}

func writeInt(buf *bytes.Buffer, n int) {
	var scratch [20]byte
	buf.Write(strconv.AppendInt(scratch[:0], int64(n), 10))
}

func ghostSuffix(query string, results []engine.Result) string {
	if len(results) == 0 || results[0].Product == nil {
		return ""
	}
	name := results[0].Product.Name
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

	if id < 0 || id >= app.productCount {
		http.Error(w, "product ID out of range", http.StatusBadRequest)
		return
	}

	app.Engine.RecordSelection(id)
	app.Cache.Invalidate()
	w.WriteHeader(http.StatusOK)
}

// FragmentCache is a cache for rendered HTML fragments.
// Keyed by query string, invalidated when selections change popularity.
// When full, evicts a random half of entries instead of a full flush.
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
// When full, randomly evicts half the entries to resist cache flooding.
func (c *FragmentCache) Set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxSize {
		// Evict ~half. Map iteration order is randomized in Go,
		// so deleting the first half yields random eviction.
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

// Invalidate clears all cached fragments.
func (c *FragmentCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

// RateLimiter implements a per-IP token bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	burst    int           // max tokens (burst capacity)
	rate     float64       // tokens per second (sustained rate)
	ttl      time.Duration // time before idle entries are cleaned up
}

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

// NewRateLimiter creates a rate limiter with the given burst size,
// sustained rate (requests/sec), and cleanup TTL for idle IPs.
func NewRateLimiter(burst int, rate float64, ttl time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		burst:    burst,
		rate:     rate,
		ttl:      ttl,
	}
	return rl
}

// Allow checks whether a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[ip]
	if !ok {
		rl.visitors[ip] = &visitor{tokens: float64(rl.burst) - 1, lastSeen: now}
		// Periodically clean up stale entries.
		if len(rl.visitors)%100 == 0 {
			rl.cleanupLocked(now)
		}
		return true
	}

	// Replenish tokens based on time elapsed.
	elapsed := now.Sub(v.lastSeen).Seconds()
	v.tokens = min(float64(rl.burst), v.tokens+elapsed*rl.rate)
	v.lastSeen = now

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

// cleanupLocked removes visitors that haven't been seen within the TTL.
// Caller must hold rl.mu.
func (rl *RateLimiter) cleanupLocked(now time.Time) {
	maps.DeleteFunc(rl.visitors, func(_ string, v *visitor) bool {
		return now.Sub(v.lastSeen) > rl.ttl
	})
}

// Wrap returns middleware that rate-limits by client IP.
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

// clientIP extracts the client IP from the request, preferring
// X-Forwarded-For if behind a reverse proxy, falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	// RemoteAddr is "ip:port" — strip the port.
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}
