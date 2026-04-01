// Package engine orchestrates the search pipeline: Bloom filter fast rejection,
// n-gram index lookup with Jaccard scoring, category fallback, and popularity
// ranking with exponential time decay.
package engine

import (
	"cmp"
	"fmt"
	"html"
	"maps"
	"github.com/mbow/go-xsearch/bm25"
	"github.com/mbow/go-xsearch/bloom"
	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/index"
	"github.com/mbow/go-xsearch/ranking"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

// MatchType indicates how a result was found.
type MatchType int

const (
	MatchDirect   MatchType = iota // Found via direct n-gram match
	MatchFallback                  // Found via category fallback
)

// Highlight marks a matched byte range within a product name.
type Highlight struct {
	Start int // byte offset (inclusive)
	End   int // byte offset (exclusive)
}

// Result is a single search result with metadata.
type Result struct {
	Product         catalog.Product
	ProductID       int
	Score           float64
	MatchType       MatchType
	Highlights      []Highlight
	HighlightedName string
}

// Engine orchestrates search across all components.
type Engine struct {
	products      []catalog.Product
	bloom         *bloom.Filter
	index         *index.Index
	ranker        *ranking.Ranker
	bm25idx       *bm25.Index
	prefixCache   map[string][]Result // precomputed results for 1-2 char prefixes
	categoryCache map[string][]Result // precomputed top-N per category
	catCacheDirty bool                // true when selections have changed
	mu            sync.RWMutex        // protects categoryCache and catCacheDirty
}

const (
	bloomSize         = 20000
	bloomHashCount    = 3
	lambda            = 0.05
	alpha             = 0.6
	fallbackThreshold = 0.2
	minDirectResults  = 3
	fallbackRelevance = 0.1
	maxResults        = 10
	bm25Alpha         = 0.7 // BM25 score weight (includes internal prefix bonus)
	bm25PopAlpha      = 0.3 // popularity weight in BM25 path
)

// New creates a search engine from the given product catalog.
func New(products []catalog.Product) *Engine {
	e := &Engine{
		products: products,
		bloom:    bloom.New(bloomSize, bloomHashCount),
		index:    index.NewIndex(products),
		ranker:   ranking.New(lambda, alpha),
		bm25idx:  bm25.NewIndex(products),
	}

	// Populate Bloom filter with all product, category, and tag trigrams
	for _, p := range products {
		for _, g := range index.ExtractTrigrams(p.Name) {
			e.bloom.Add(g)
		}
		for _, g := range index.ExtractTrigrams(p.Category) {
			e.bloom.Add(g)
		}
		for _, tag := range p.Tags {
			for _, g := range index.ExtractTrigrams(tag) {
				e.bloom.Add(g)
			}
		}
	}

	// Precompute caches
	e.buildPrefixCache()
	e.rebuildCategoryCache()

	return e
}

// NewFromEmbedded creates an engine using pre-built bloom filter and index
// deserialized from raw CBOR bytes. Skips index construction entirely.
func NewFromEmbedded(products []catalog.Product, bloomRaw, indexRaw, bm25Raw []byte) (*Engine, error) {
	var bs bloom.Snapshot
	if err := cbor.Unmarshal(bloomRaw, &bs); err != nil {
		return nil, fmt.Errorf("unmarshaling bloom snapshot: %w", err)
	}

	var is index.Snapshot
	if err := cbor.Unmarshal(indexRaw, &is); err != nil {
		return nil, fmt.Errorf("unmarshaling index snapshot: %w", err)
	}

	var bm25s bm25.Snapshot
	if err := cbor.Unmarshal(bm25Raw, &bm25s); err != nil {
		return nil, fmt.Errorf("unmarshaling bm25 snapshot: %w", err)
	}

	bm25Idx, err := bm25.FromSnapshot(bm25s)
	if err != nil {
		return nil, fmt.Errorf("restoring bm25 index: %w", err)
	}

	e := &Engine{
		products: products,
		bloom:    bloom.FromSnapshot(bs),
		index:    index.FromSnapshot(is, products),
		ranker:   ranking.New(lambda, alpha),
		bm25idx:  bm25Idx,
	}

	e.buildPrefixCache()
	e.rebuildCategoryCache()

	return e, nil
}

// buildPrefixCache precomputes search results for every 1-char and 2-char
// prefix that matches at least one product. These are the highest-traffic
// queries since every search starts with 1-2 characters.
func (e *Engine) buildPrefixCache() {
	prefixSet := make(map[string]struct{})
	for _, p := range e.products {
		name := strings.ToLower(p.Name)
		if len(name) >= 1 {
			prefixSet[name[:1]] = struct{}{}
		}
		if len(name) >= 2 {
			prefixSet[name[:2]] = struct{}{}
		}
	}

	prefixes := make([]string, 0, len(prefixSet))
	for p := range prefixSet {
		prefixes = append(prefixes, p)
	}

	type entry struct {
		prefix  string
		results []Result
	}

	workers := runtime.GOMAXPROCS(0)
	ch := make(chan string, len(prefixes))
	resultCh := make(chan entry, len(prefixes))

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for prefix := range ch {
				results := e.searchInternal(prefix)
				if len(results) > 0 {
					resultCh <- entry{prefix: prefix, results: results}
				}
			}
		}()
	}

	for _, p := range prefixes {
		ch <- p
	}
	close(ch)
	wg.Wait()
	close(resultCh)

	e.prefixCache = make(map[string][]Result, len(prefixes))
	for ent := range resultCh {
		e.prefixCache[ent.prefix] = ent.results
	}
}

// Search returns ranked results for the given query.
func (e *Engine) Search(query string) []Result {
	if query == "" {
		return nil
	}

	query = strings.ToLower(strings.TrimSpace(query))

	// Check prefix cache for 1-2 char queries
	if len(query) <= 2 {
		if cached, ok := e.prefixCache[query]; ok {
			return cached
		}
	}

	// Check if query exactly matches a category name — return precomputed top-N
	e.mu.RLock()
	if e.catCacheDirty {
		e.mu.RUnlock()
		e.mu.Lock()
		if e.catCacheDirty {
			e.rebuildCategoryCache()
		}
		e.mu.Unlock()
		e.mu.RLock()
	}
	if cached, ok := e.categoryCache[query]; ok {
		e.mu.RUnlock()
		return cached
	}
	e.mu.RUnlock()

	return e.searchInternal(query)
}

// searchInternal performs the full search pipeline.
func (e *Engine) searchInternal(query string) []Result {
	score := e.ranker.Scorer()

	// Try BM25 path first (word-level matching)
	bm25Results := e.bm25idx.Search(query)
	if len(bm25Results) > 0 {
		return e.buildBM25Results(bm25Results, score, query)
	}

	// Fallback to Jaccard trigram path (handles typos/fuzzy)
	trigrams := index.ExtractTrigrams(query)
	if len(trigrams) == 0 {
		return e.buildResults(e.index.Search(query), MatchDirect, score, query)
	}

	anyPass := false
	for _, g := range trigrams {
		if e.bloom.MayContain(g) {
			anyPass = true
			break
		}
	}

	results := make([]Result, 0, maxResults)

	if anyPass {
		searchResults := e.index.Search(query)
		goodResults := 0
		for _, sr := range searchResults {
			if sr.Score >= fallbackThreshold {
				goodResults++
			}
		}
		results = append(results, e.buildResults(searchResults, MatchDirect, score, query)...)
		if goodResults < minDirectResults {
			fallbackResults := e.categoryFallback(query, searchResults, score)
			results = append(results, fallbackResults...)
		}
	} else {
		results = append(results, e.categoryFallback(query, nil, score)...)
	}

	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Compare(b.Score, a.Score)
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// RecordSelection records a user selecting a product.
func (e *Engine) RecordSelection(productID int) {
	e.ranker.RecordSelection(productID)
	e.mu.Lock()
	e.catCacheDirty = true
	e.mu.Unlock()
}

// Ranker returns the underlying ranker for persistence operations.
func (e *Engine) Ranker() *ranking.Ranker {
	return e.ranker
}

// scorerFunc scores a product given its ID and relevance.
type scorerFunc func(productID int, relevance float64) float64

// computeHighlights finds byte positions of query matches in the product name.
// queryWords should be pre-split and lowercased to avoid repeated allocation.
// query is the full lowercased query string used as a substring fallback.
func computeHighlights(name string, queryWords []string, query string) []Highlight {
	lowerName := strings.ToLower(name)

	// Try to find each query word in the product name.
	var highlights []Highlight
	for _, word := range queryWords {
		pos := strings.Index(lowerName, word)
		if pos >= 0 {
			highlights = append(highlights, Highlight{Start: pos, End: pos + len(word)})
		}
	}

	// If no word matches, try the full query as a substring.
	if len(highlights) == 0 {
		pos := strings.Index(lowerName, query)
		if pos >= 0 {
			highlights = append(highlights, Highlight{Start: pos, End: pos + len(query)})
		}
	}

	// Sort by start position and merge overlaps.
	slices.SortFunc(highlights, func(a, b Highlight) int {
		return cmp.Compare(a.Start, b.Start)
	})
	return mergeHighlights(highlights)
}

// mergeHighlights merges overlapping or adjacent highlight ranges.
func mergeHighlights(hs []Highlight) []Highlight {
	if len(hs) <= 1 {
		return hs
	}
	merged := []Highlight{hs[0]}
	for _, h := range hs[1:] {
		last := &merged[len(merged)-1]
		if h.Start <= last.End {
			last.End = max(last.End, h.End)
		} else {
			merged = append(merged, h)
		}
	}
	return merged
}

// buildHighlightedName renders product name with <mark> tags around matched portions.
func buildHighlightedName(name string, highlights []Highlight) string {
	if len(highlights) == 0 {
		return html.EscapeString(name)
	}

	var b strings.Builder
	b.Grow(len(name) + len(highlights)*13) // 13 = len("<mark>") + len("</mark>")
	prev := 0
	for _, h := range highlights {
		if h.Start > prev {
			b.WriteString(html.EscapeString(name[prev:h.Start]))
		}
		b.WriteString("<mark>")
		b.WriteString(html.EscapeString(name[h.Start:h.End]))
		b.WriteString("</mark>")
		prev = h.End
	}
	if prev < len(name) {
		b.WriteString(html.EscapeString(name[prev:]))
	}
	return b.String()
}

// buildBM25Results converts BM25 search results to engine results,
// normalizing BM25 scores and blending with prefix boost and popularity.
func (e *Engine) buildBM25Results(bm25Results []bm25.SearchResult, score scorerFunc, query string) []Result {
	maxBM25 := 0.0
	for _, r := range bm25Results {
		if r.Score > maxBM25 {
			maxBM25 = r.Score
		}
	}
	if maxBM25 == 0 {
		maxBM25 = 1.0
	}

	// Pre-split query words once to avoid per-result strings.Fields allocation.
	queryWords := strings.Fields(query)

	results := make([]Result, 0, min(len(bm25Results), maxResults))
	for _, r := range bm25Results {
		if r.ProductID < 0 || r.ProductID >= len(e.products) {
			continue
		}

		normalizedBM25 := r.Score / maxBM25
		popScore := score(r.ProductID, 0)
		combined := bm25Alpha*normalizedBM25 + bm25PopAlpha*popScore

		hl := computeHighlights(e.products[r.ProductID].Name, queryWords, query)
		results = append(results, Result{
			Product:         e.products[r.ProductID],
			ProductID:       r.ProductID,
			Score:           combined,
			MatchType:       MatchDirect,
			Highlights:      hl,
			HighlightedName: buildHighlightedName(e.products[r.ProductID].Name, hl),
		})
	}

	// Sort by combined score (popularity may reorder BM25 ranking)
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Compare(b.Score, a.Score)
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// buildResults converts index search results to engine results with combined scores.
func (e *Engine) buildResults(searchResults []index.SearchResult, matchType MatchType, score scorerFunc, query string) []Result {
	// Pre-split query words once to avoid per-result strings.Fields allocation.
	queryWords := strings.Fields(query)

	results := make([]Result, 0, len(searchResults))
	for _, sr := range searchResults {
		if sr.ProductID < 0 || sr.ProductID >= len(e.products) {
			continue
		}
		s := score(sr.ProductID, sr.Score)
		hl := computeHighlights(e.products[sr.ProductID].Name, queryWords, query)
		results = append(results, Result{
			Product:         e.products[sr.ProductID],
			ProductID:       sr.ProductID,
			Score:           s,
			MatchType:       matchType,
			Highlights:      hl,
			HighlightedName: buildHighlightedName(e.products[sr.ProductID].Name, hl),
		})
	}
	return results
}

// categoryFallback finds the best matching category and returns its top products.
// Uses a precomputed cache that is rebuilt when popularity changes.
func (e *Engine) categoryFallback(query string, exclude []index.SearchResult, score scorerFunc) []Result {
	categories := e.index.SearchCategories(query)
	if len(categories) == 0 {
		return nil
	}

	bestCat := categories[0]

	// Check/rebuild category cache
	e.mu.Lock()
	if e.catCacheDirty || e.categoryCache == nil {
		e.rebuildCategoryCache()
	}
	cached := e.categoryCache[bestCat]
	e.mu.Unlock()

	if len(cached) == 0 {
		return nil
	}

	// Build set of already-found product IDs to avoid duplicates
	seen := make(map[int]struct{}, len(exclude))
	for _, sr := range exclude {
		seen[sr.ProductID] = struct{}{}
	}

	// Filter out already-seen products from cached results
	results := make([]Result, 0, len(cached))
	for _, r := range cached {
		if _, ok := seen[r.ProductID]; ok {
			continue
		}
		results = append(results, r)
	}

	return results
}

// rebuildCategoryCache precomputes the top maxResults products per category.
// Caller must hold e.mu lock.
func (e *Engine) rebuildCategoryCache() {
	score := e.ranker.Scorer()
	cache := make(map[string][]Result)

	// Get all unique categories
	for _, cat := range e.allCategories() {
		productIDs := e.index.ProductsByCategory(cat)
		if len(productIDs) == 0 {
			continue
		}

		// Score all products in category
		scored := make([]Result, 0, min(len(productIDs), maxResults*2))
		catWords := strings.Fields(cat)
		for _, id := range productIDs {
			if id < 0 || id >= len(e.products) {
				continue
			}
			s := score(id, fallbackRelevance)
			name := e.products[id].Name
			hl := computeHighlights(name, catWords, cat)
			scored = append(scored, Result{
				Product:         e.products[id],
				ProductID:       id,
				Score:           s,
				MatchType:       MatchFallback,
				Highlights:      hl,
				HighlightedName: buildHighlightedName(name, hl),
			})
		}

		// Keep only top maxResults using partial sort via heap-select pattern
		if len(scored) > maxResults {
			slices.SortFunc(scored, func(a, b Result) int {
				return cmp.Compare(b.Score, a.Score)
			})
			scored = scored[:maxResults]
		}

		cache[cat] = scored
	}

	e.categoryCache = cache
	e.catCacheDirty = false
}

// allCategories returns all unique category names.
func (e *Engine) allCategories() []string {
	return slices.Collect(maps.Keys(e.index.CategoryNames()))
}
