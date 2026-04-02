// Package engine orchestrates the search pipeline: Bloom filter fast rejection,
// n-gram index lookup with Jaccard scoring, category fallback, and popularity
// ranking with exponential time decay.
package engine

import (
	"cmp"
	"fmt"
	"github.com/mbow/go-xsearch/bloom"
	"github.com/mbow/go-xsearch/bm25"
	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/index"
	"github.com/mbow/go-xsearch/ranking"
	"html"
	"html/template"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

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

const maxHighlights = 4

// Result is a single search result with metadata.
type Result struct {
	Product         catalog.Product
	ProductID       int
	Score           float64
	MatchType       MatchType
	Highlights      [maxHighlights]Highlight
	HighlightCount  uint8
	HighlightedName template.HTML // security: only buildHighlightedName constructs this; all segments are html.EscapeString'd
}

// Engine orchestrates search across all components.
type Engine struct {
	products      []catalog.Product
	lowerNames    []string // pre-lowered product names
	bloom         *bloom.Filter
	index         *index.Index
	ranker        *ranking.Ranker
	bm25idx       *bm25.Index
	prefixCache   map[string][]Result // precomputed results for 1-2 char prefixes
	categoryCache map[string][]Result // precomputed top-N per category
	dirtyCats     map[string]struct{} // categories requiring cache refresh
	mu            sync.RWMutex        // protects categoryCache and dirtyCats
}

const (
	bloomMinSize      = 20000
	bloomBitsPerItem  = 100 // ~100 bits per product for low false-positive rate
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
		bloom:    bloom.New(max(bloomMinSize, uint64(len(products))*bloomBitsPerItem), bloomHashCount),
		index:    index.NewIndex(products),
		ranker:   ranking.New(lambda, alpha, len(products)),
		bm25idx:  bm25.NewIndex(products),
	}

	e.lowerNames = make([]string, len(products))
	for i, p := range products {
		e.lowerNames[i] = strings.ToLower(p.Name)
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
		ranker:   ranking.New(lambda, alpha, len(products)),
		bm25idx:  bm25Idx,
	}

	e.lowerNames = make([]string, len(products))
	for i, p := range products {
		e.lowerNames[i] = strings.ToLower(p.Name)
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
	for _, name := range e.lowerNames {
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
		wg.Go(func() {
			for prefix := range ch {
				results := e.searchInternal(prefix)
				if len(results) > 0 {
					resultCh <- entry{prefix: prefix, results: results}
				}
			}
		})
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
	query = normalizeQuery(query)
	if query == "" {
		return nil
	}

	// Check prefix cache for 1-2 char queries
	if len(query) <= 2 {
		if cached, ok := e.prefixCache[query]; ok {
			return cached
		}
	}

	// Check if query exactly matches a category name — return precomputed top-N.
	score := e.ranker.ScoreView()
	if cached, ok := e.ensureCategoryCached(query, score); ok {
		return cached
	}

	return e.searchInternal(query)
}

// searchInternal performs the full search pipeline.
func (e *Engine) searchInternal(query string) []Result {
	score := e.ranker.ScoreView()

	// Extract trigrams once — reused by Bloom check, Jaccard search, and category lookup.
	var trigramBuf [16]string
	trigrams := extractNormalizedTrigrams(trigramBuf[:0], query)

	// Short queries (< 3 chars): no trigrams available, try BM25 then prefix search.
	if len(trigrams) == 0 {
		bm25Results := e.bm25idx.Search(query)
		if len(bm25Results) > 0 {
			return e.buildBM25Results(bm25Results, score, query)
		}
		results := e.buildResults(e.index.Search(query), MatchDirect, score)
		e.addHighlights(results, query)
		return results
	}

	// Bloom pre-check: if no trigrams pass, skip BM25 and Jaccard entirely.
	// Only category fallback is possible (reusing pre-extracted trigrams).
	anyPass := false
	for _, g := range trigrams {
		if e.bloom.MayContain(g) {
			anyPass = true
			break
		}
	}
	if !anyPass {
		if bestCat, ok := e.index.BestCategoryWithGrams(trigrams); ok {
			return e.categoryFallback(bestCat, nil, score)
		}
		return nil
	}

	// Bloom passed — try BM25 first (primary path for well-formed queries).
	bm25Results := e.bm25idx.Search(query)
	if len(bm25Results) > 0 {
		return e.buildBM25Results(bm25Results, score, query)
	}

	// BM25 miss — Jaccard trigram fallback (handles typos/fuzzy).
	// Passes pre-extracted trigrams to avoid recomputing them.
	searchResults := e.index.SearchWithGrams(trigrams)

	goodResults := 0
	for _, sr := range searchResults {
		if sr.Score >= fallbackThreshold {
			goodResults++
		}
	}

	results := make([]Result, 0, maxResults)
	for _, sr := range searchResults {
		if sr.ProductID < 0 || sr.ProductID >= len(e.products) {
			continue
		}
		results = pushTopResult(results, Result{
			Product:   e.products[sr.ProductID],
			ProductID: sr.ProductID,
			Score:     score.Score(sr.ProductID, sr.Score),
			MatchType: MatchDirect,
		})
	}

	if goodResults < minDirectResults {
		if bestCat, ok := e.index.BestCategoryWithGrams(trigrams); ok {
			if cached, ok := e.ensureCategoryCached(bestCat, score); ok {
				seen := make(map[int]struct{}, len(searchResults))
				for _, sr := range searchResults {
					seen[sr.ProductID] = struct{}{}
				}
				for _, r := range cached {
					if _, ok := seen[r.ProductID]; ok {
						continue
					}
					results = pushTopResult(results, r)
				}
			}
		}
	}

	if len(results) == 0 {
		return nil
	}
	results = drainTopResults(results)

	// Compute highlights only for final top-K results.
	e.addHighlights(results, query)

	return results
}

// RecordSelection records a user selecting a product.
func (e *Engine) RecordSelection(productID int) {
	e.ranker.RecordSelection(productID)
	if productID < 0 || productID >= len(e.products) {
		return
	}
	category := e.products[productID].Category
	e.mu.Lock()
	if e.dirtyCats == nil {
		e.dirtyCats = make(map[string]struct{})
	}
	e.dirtyCats[category] = struct{}{}
	e.mu.Unlock()
}

// Ranker returns the underlying ranker for persistence operations.
func (e *Engine) Ranker() *ranking.Ranker {
	return e.ranker
}

// computeHighlights finds byte positions of query matches in the product name.
// query is the full lowercased query string used as a substring fallback.
func computeHighlights(lowerName, query string) ([maxHighlights]Highlight, uint8) {
	var highlights [maxHighlights]Highlight
	var count uint8

	forEachQueryWord(query, func(word string) {
		pos := strings.Index(lowerName, word)
		if pos < 0 || count == maxHighlights {
			return
		}
		highlights[count] = Highlight{Start: pos, End: pos + len(word)}
		count++
	})

	if count == 0 {
		pos := strings.Index(lowerName, query)
		if pos >= 0 {
			highlights[0] = Highlight{Start: pos, End: pos + len(query)}
			return highlights, 1
		}
		return highlights, 0
	}

	return mergeHighlights(highlights, count)
}

// mergeHighlights merges overlapping or adjacent highlight ranges.
func mergeHighlights(hs [maxHighlights]Highlight, count uint8) ([maxHighlights]Highlight, uint8) {
	if count <= 1 {
		return hs, count
	}

	sortHighlights(&hs, int(count))

	merged := 1
	for i := 1; i < int(count); i++ {
		last := &hs[merged-1]
		if hs[i].Start <= last.End {
			last.End = max(last.End, hs[i].End)
		} else {
			hs[merged] = hs[i]
			merged++
		}
	}
	return hs, uint8(merged)
}

func sortHighlights(hs *[maxHighlights]Highlight, count int) {
	for i := 1; i < count; i++ {
		cur := hs[i]
		j := i - 1
		for ; j >= 0 && hs[j].Start > cur.Start; j-- {
			hs[j+1] = hs[j]
		}
		hs[j+1] = cur
	}
}

// buildHighlightedName renders product name with <mark> tags around matched portions.
func buildHighlightedName(name string, highlights [maxHighlights]Highlight, count uint8) template.HTML {
	if count == 0 {
		return template.HTML(html.EscapeString(name))
	}

	var b strings.Builder
	b.Grow(len(name) + int(count)*13) // 13 = len("<mark>") + len("</mark>")
	prev := 0
	for i := 0; i < int(count); i++ {
		h := highlights[i]
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
	return template.HTML(b.String())
}

// buildBM25Results converts BM25 search results to engine results,
// normalizing BM25 scores and blending with prefix boost and popularity.
func (e *Engine) buildBM25Results(bm25Results []bm25.SearchResult, score ranking.ScoreView, query string) []Result {
	maxBM25 := 0.0
	for _, r := range bm25Results {
		if r.Score > maxBM25 {
			maxBM25 = r.Score
		}
	}
	if maxBM25 == 0 {
		maxBM25 = 1.0
	}

	// Score all candidates without computing highlights (avoids per-result allocations).
	results := make([]Result, 0, min(len(bm25Results), maxResults))
	for _, r := range bm25Results {
		if r.ProductID < 0 || r.ProductID >= len(e.products) {
			continue
		}

		normalizedBM25 := r.Score / maxBM25
		popScore := score.Score(r.ProductID, 0)
		combined := bm25Alpha*normalizedBM25 + bm25PopAlpha*popScore

		results = append(results, Result{
			Product:   e.products[r.ProductID],
			ProductID: r.ProductID,
			Score:     combined,
			MatchType: MatchDirect,
		})
	}

	// Sort by combined score (popularity may reorder BM25 ranking)
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Compare(b.Score, a.Score)
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	// Compute highlights only for the final top-K results to minimize allocations.
	for i := range results {
		highlights, count := computeHighlights(e.lowerNames[results[i].ProductID], query)
		results[i].Highlights = highlights
		results[i].HighlightCount = count
		results[i].HighlightedName = buildHighlightedName(results[i].Product.Name, highlights, count)
	}

	return results
}

// buildResults converts index search results to engine results with combined scores.
// Highlights are deferred — call addHighlights on the final results after truncation.
func (e *Engine) buildResults(searchResults []index.SearchResult, matchType MatchType, score ranking.ScoreView) []Result {
	results := make([]Result, 0, len(searchResults))
	for _, sr := range searchResults {
		if sr.ProductID < 0 || sr.ProductID >= len(e.products) {
			continue
		}
		s := score.Score(sr.ProductID, sr.Score)
		results = append(results, Result{
			Product:   e.products[sr.ProductID],
			ProductID: sr.ProductID,
			Score:     s,
			MatchType: matchType,
		})
	}
	return results
}

// addHighlights computes match highlighting for a slice of results.
// Call this only on the final top-K results to minimize allocations.
func (e *Engine) addHighlights(results []Result, query string) {
	if len(results) == 0 {
		return
	}
	for i := range results {
		highlights, count := computeHighlights(e.lowerNames[results[i].ProductID], query)
		results[i].Highlights = highlights
		results[i].HighlightCount = count
		results[i].HighlightedName = buildHighlightedName(results[i].Product.Name, highlights, count)
	}
}

// categoryFallback returns cached products for the chosen category.
func (e *Engine) categoryFallback(category string, exclude []index.SearchResult, score ranking.ScoreView) []Result {
	if category == "" {
		return nil
	}
	cached, ok := e.ensureCategoryCached(category, score)
	if !ok {
		return nil
	}

	if len(cached) == 0 {
		return nil
	}
	if len(exclude) == 0 {
		return cached
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

func (e *Engine) ensureCategoryCached(category string, score ranking.ScoreView) ([]Result, bool) {
	e.mu.RLock()
	cached, ok := e.categoryCache[category]
	_, dirty := e.dirtyCats[category]
	e.mu.RUnlock()
	if ok && !dirty {
		return cached, true
	}

	productIDs := e.index.ProductsByCategory(category)
	if len(productIDs) == 0 {
		return nil, false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	cached, ok = e.categoryCache[category]
	_, dirty = e.dirtyCats[category]
	if ok && !dirty {
		return cached, true
	}

	if e.categoryCache == nil {
		e.categoryCache = make(map[string][]Result)
	}
	cached = e.buildCategoryResults(category, score)
	e.categoryCache[category] = cached
	delete(e.dirtyCats, category)
	return cached, true
}

func (e *Engine) buildCategoryResults(category string, score ranking.ScoreView) []Result {
	productIDs := e.index.ProductsByCategory(category)
	if len(productIDs) == 0 {
		return nil
	}

	scored := make([]Result, 0, min(len(productIDs), maxResults*2))
	for _, id := range productIDs {
		if id < 0 || id >= len(e.products) {
			continue
		}
		s := score.Score(id, fallbackRelevance)
		name := e.products[id].Name
		highlights, count := computeHighlights(e.lowerNames[id], category)
		scored = append(scored, Result{
			Product:         e.products[id],
			ProductID:       id,
			Score:           s,
			MatchType:       MatchFallback,
			Highlights:      highlights,
			HighlightCount:  count,
			HighlightedName: buildHighlightedName(name, highlights, count),
		})
	}

	if len(scored) > maxResults {
		slices.SortFunc(scored, func(a, b Result) int {
			return cmp.Compare(b.Score, a.Score)
		})
		scored = scored[:maxResults]
	}

	return scored
}

// rebuildCategoryCache precomputes the top maxResults products per category.
// Caller must hold e.mu lock.
func (e *Engine) rebuildCategoryCache() {
	score := e.ranker.ScoreView()
	cache := make(map[string][]Result)

	// Get all unique categories
	for _, cat := range e.allCategories() {
		cache[cat] = e.buildCategoryResults(cat, score)
	}

	e.categoryCache = cache
	e.dirtyCats = make(map[string]struct{})
}

// allCategories returns all unique category names.
func (e *Engine) allCategories() []string {
	return slices.Collect(e.index.CategoryNames())
}

func normalizeQuery(s string) string {
	start := 0
	end := len(s)
	for start < end && isASCIIWhitespace(s[start]) {
		start++
	}
	for start < end && isASCIIWhitespace(s[end-1]) {
		end--
	}
	s = s[start:end]
	if s == "" {
		return ""
	}
	for i := range len(s) {
		c := s[i]
		if c >= utf8.RuneSelf || (c >= 'A' && c <= 'Z') {
			return strings.ToLower(s)
		}
	}
	return s
}

func isASCIIWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func extractNormalizedTrigrams(dst []string, s string) []string {
	if len(s) < 3 {
		return nil
	}
	need := len(s) - 2
	if cap(dst) < need {
		dst = make([]string, 0, need)
	} else {
		dst = dst[:0]
	}
	for i := range need {
		dst = append(dst, s[i:i+3])
	}
	return dst
}

func forEachQueryWord(query string, yield func(word string)) {
	start := -1
	for i := 0; i <= len(query); i++ {
		if i < len(query) && !isASCIIWhitespace(query[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			yield(query[start:i])
			start = -1
		}
	}
}

func lessResult(a, b Result) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return a.ProductID > b.ProductID
}

func siftDownResults(h []Result, root int) {
	for {
		child := 2*root + 1
		if child >= len(h) {
			return
		}
		if child+1 < len(h) && lessResult(h[child+1], h[child]) {
			child++
		}
		if !lessResult(h[child], h[root]) {
			return
		}
		h[root], h[child] = h[child], h[root]
		root = child
	}
}

func pushTopResult(h []Result, r Result) []Result {
	if len(h) < maxResults {
		h = append(h, r)
		for i := len(h) - 1; i > 0; {
			parent := (i - 1) / 2
			if !lessResult(h[i], h[parent]) {
				break
			}
			h[i], h[parent] = h[parent], h[i]
			i = parent
		}
		return h
	}
	if !lessResult(h[0], r) {
		return h
	}
	h[0] = r
	siftDownResults(h, 0)
	return h
}

func drainTopResults(h []Result) []Result {
	if len(h) == 0 {
		return nil
	}
	results := make([]Result, len(h))
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = h[0]
		last := len(h) - 1
		h[0] = h[last]
		h = h[:last]
		if len(h) > 0 {
			siftDownResults(h, 0)
		}
	}
	return results
}
