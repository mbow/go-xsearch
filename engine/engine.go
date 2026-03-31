package engine

import (
	"search/bloom"
	"search/catalog"
	"search/index"
	"search/ranking"
	"sort"
	"strings"
	"sync"
)

// MatchType indicates how a result was found.
type MatchType int

const (
	MatchDirect   MatchType = iota // Found via direct n-gram match
	MatchFallback                  // Found via category fallback
)

// Result is a single search result with metadata.
type Result struct {
	Product   catalog.Product
	ProductID int
	Score     float64
	MatchType MatchType
}

// Engine orchestrates search across all components.
type Engine struct {
	products    []catalog.Product
	bloom       *bloom.Filter
	index       *index.Index
	ranker      *ranking.Ranker
	prefixCache map[string][]Result // precomputed results for 1-2 char prefixes
	resultPool  sync.Pool
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
)

// New creates a search engine from the given product catalog.
func New(products []catalog.Product) *Engine {
	e := &Engine{
		products: products,
		bloom:    bloom.New(bloomSize, bloomHashCount),
		index:    index.NewIndex(products),
		ranker:   ranking.New(lambda, alpha),
		resultPool: sync.Pool{
			New: func() any { s := make([]Result, 0, maxResults); return &s },
		},
	}

	// Populate Bloom filter with all product and category trigrams
	for _, p := range products {
		for _, g := range index.ExtractTrigrams(p.Name) {
			e.bloom.Add(g)
		}
		for _, g := range index.ExtractTrigrams(p.Category) {
			e.bloom.Add(g)
		}
	}

	// Precompute prefix results for all 1-2 character prefixes
	e.buildPrefixCache()

	return e
}

// buildPrefixCache precomputes search results for every 1-char and 2-char
// prefix that matches at least one product. These are the highest-traffic
// queries since every search starts with 1-2 characters.
func (e *Engine) buildPrefixCache() {
	e.prefixCache = make(map[string][]Result)

	// Collect unique 1-char and 2-char prefixes from product names
	prefixes := make(map[string]struct{})
	for _, p := range e.products {
		name := strings.ToLower(p.Name)
		if len(name) >= 1 {
			prefixes[name[:1]] = struct{}{}
		}
		if len(name) >= 2 {
			prefixes[name[:2]] = struct{}{}
		}
	}

	// Precompute results for each prefix
	for prefix := range prefixes {
		results := e.searchInternal(prefix)
		if len(results) > 0 {
			e.prefixCache[prefix] = results
		}
	}
}

// getResults gets a result slice from the pool.
func (e *Engine) getResults() *[]Result {
	return e.resultPool.Get().(*[]Result)
}

// putResults returns a result slice to the pool.
func (e *Engine) putResults(r *[]Result) {
	*r = (*r)[:0]
	e.resultPool.Put(r)
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

	return e.searchInternal(query)
}

// searchInternal performs the full search pipeline.
func (e *Engine) searchInternal(query string) []Result {
	trigrams := index.ExtractTrigrams(query)

	// Short query bypass — skip Bloom, go straight to prefix search
	if len(trigrams) == 0 {
		return e.buildResults(e.index.Search(query), MatchDirect)
	}

	// Bloom filter check — fast rejection
	anyPass := false
	for _, g := range trigrams {
		if e.bloom.MayContain(g) {
			anyPass = true
			break
		}
	}

	// Get a pooled result slice
	pooled := e.getResults()
	results := *pooled

	if anyPass {
		searchResults := e.index.Search(query)

		// Count how many pass the quality threshold
		goodResults := 0
		for _, sr := range searchResults {
			if sr.Score >= fallbackThreshold {
				goodResults++
			}
		}

		results = append(results, e.buildResults(searchResults, MatchDirect)...)

		// If not enough good direct results, add category fallback
		if goodResults < minDirectResults {
			fallbackResults := e.categoryFallback(query, searchResults)
			results = append(results, fallbackResults...)
		}
	} else {
		// Bloom rejected everything — try category fallback only
		results = append(results, e.categoryFallback(query, nil)...)
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Limit results
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	// Copy out of pooled slice so we can return it to pool
	out := make([]Result, len(results))
	copy(out, results)

	*pooled = results
	e.putResults(pooled)

	return out
}

// RecordSelection records a user selecting a product.
func (e *Engine) RecordSelection(productID int) {
	e.ranker.RecordSelection(productID)
}

// Ranker returns the underlying ranker for persistence operations.
func (e *Engine) Ranker() *ranking.Ranker {
	return e.ranker
}

// buildResults converts index search results to engine results with combined scores.
func (e *Engine) buildResults(searchResults []index.SearchResult, matchType MatchType) []Result {
	results := make([]Result, 0, len(searchResults))
	for _, sr := range searchResults {
		if sr.ProductID < 0 || sr.ProductID >= len(e.products) {
			continue
		}
		score := e.ranker.CombinedScore(sr.ProductID, sr.Score)
		results = append(results, Result{
			Product:   e.products[sr.ProductID],
			ProductID: sr.ProductID,
			Score:     score,
			MatchType: matchType,
		})
	}
	return results
}

// categoryFallback finds the best matching category and returns its popular products.
func (e *Engine) categoryFallback(query string, exclude []index.SearchResult) []Result {
	categories := e.index.SearchCategories(query)
	if len(categories) == 0 {
		return nil
	}

	// Build set of already-found product IDs to avoid duplicates
	seen := make(map[int]struct{})
	for _, sr := range exclude {
		seen[sr.ProductID] = struct{}{}
	}

	var results []Result
	// Use the best matching category
	productIDs := e.index.ProductsByCategory(categories[0])
	for _, id := range productIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		if id < 0 || id >= len(e.products) {
			continue
		}
		score := e.ranker.CombinedScore(id, fallbackRelevance)
		results = append(results, Result{
			Product:   e.products[id],
			ProductID: id,
			Score:     score,
			MatchType: MatchFallback,
		})
	}

	return results
}
