// Package index implements an n-gram inverted index for fuzzy, prefix,
// and substring matching over a product catalog.
//
// Products are tokenized into overlapping character trigrams (3-grams).
// Queries are scored against candidates using Jaccard similarity of their
// trigram sets. For short queries (1-2 characters), a linear prefix scan
// is used instead.
package index

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/mbow/go-xsearch/catalog"
)

// ExtractTrigrams returns all overlapping 3-character substrings
// from the normalized (lowercased, trimmed) input.
// Returns nil if the input has fewer than 3 characters after normalization.
func ExtractTrigrams(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 {
		return nil
	}
	trigrams := make([]string, 0, len(s)-2)
	for i := range len(s) - 2 {
		trigrams = append(trigrams, s[i:i+3])
	}
	return trigrams
}

// SearchResult holds a product match with its Jaccard similarity score.
type SearchResult struct {
	ProductID int
	Score     float64
}

type nameEntry struct {
	Name string `cbor:"name"`
	ID   int    `cbor:"id"`
}

// Index is an n-gram inverted index over a product catalog.
type Index struct {
	products      []catalog.Product
	posting       map[string][]int               // trigram → product IDs
	docGramCounts []int                          // product ID → unique trigram count
	catTrigrams   map[string]map[string]struct{} // category → its trigram set
	catProducts   map[string][]int               // category → product IDs
	sortedNames   []nameEntry                    // sorted by lowercased name for binary search
	hitsPool      sync.Pool                      // reusable []uint8 hit counters
}

// Snapshot holds the serializable state of an [Index].
type Snapshot struct {
	Posting       map[string][]int    `cbor:"posting"`
	DocGramCounts []int               `cbor:"doc_gram_counts"`
	Trigrams      map[int][]string    `cbor:"trigrams,omitempty"` // legacy compatibility
	CatTrigrams   map[string][]string `cbor:"cat_trigrams"`
	CatProducts   map[string][]int    `cbor:"cat_products"`
	SortedNames   []nameEntry         `cbor:"sorted_names"`
}

// ToSnapshot exports the index state for serialization.
func (idx *Index) ToSnapshot() Snapshot {
	catTris := make(map[string][]string, len(idx.catTrigrams))
	for cat, grams := range idx.catTrigrams {
		catTris[cat] = slices.Collect(maps.Keys(grams))
	}

	return Snapshot{
		Posting:       idx.posting,
		DocGramCounts: idx.docGramCounts,
		CatTrigrams:   catTris,
		CatProducts:   idx.catProducts,
		SortedNames:   idx.sortedNames,
	}
}

// FromSnapshot restores an [Index] from a serialized [Snapshot] and product list.
func FromSnapshot(s Snapshot, products []catalog.Product) *Index {
	catTrigrams := make(map[string]map[string]struct{}, len(s.CatTrigrams))
	for cat, grams := range s.CatTrigrams {
		m := make(map[string]struct{}, len(grams))
		for _, g := range grams {
			m[g] = struct{}{}
		}
		catTrigrams[cat] = m
	}

	n := len(products)
	docGramCounts := make([]int, n)
	switch {
	case len(s.DocGramCounts) > 0:
		copy(docGramCounts, s.DocGramCounts)
	case len(s.Trigrams) > 0:
		for id, grams := range s.Trigrams {
			if id >= 0 && id < n {
				docGramCounts[id] = len(grams)
			}
		}
	}

	return &Index{
		products:      products,
		posting:       s.Posting,
		docGramCounts: docGramCounts,
		catTrigrams:   catTrigrams,
		catProducts:   s.CatProducts,
		sortedNames:   s.SortedNames,
		hitsPool:      sync.Pool{New: func() any { s := make([]uint8, n); return &s }},
	}
}

// NewIndex builds an n-gram inverted index from the given products.
// Each product's name and tags are tokenized into trigrams and added
// to the inverted posting lists.
func NewIndex(products []catalog.Product) *Index {
	n := len(products)
	idx := &Index{
		products:      products,
		posting:       make(map[string][]int),
		docGramCounts: make([]int, n),
		catTrigrams:   make(map[string]map[string]struct{}),
		catProducts:   make(map[string][]int),
		hitsPool:      sync.Pool{New: func() any { s := make([]uint8, n); return &s }},
	}

	for id, p := range products {
		grams := ExtractTrigrams(p.Name)
		for _, tag := range p.Tags {
			grams = append(grams, ExtractTrigrams(tag)...)
		}
		unique := make(map[string]struct{}, len(grams))
		for _, g := range grams {
			unique[g] = struct{}{}
		}
		idx.docGramCounts[id] = len(unique)
		for g := range unique {
			idx.posting[g] = append(idx.posting[g], id)
		}
		idx.catProducts[p.Category] = append(idx.catProducts[p.Category], id)
	}

	for cat := range idx.catProducts {
		grams := ExtractTrigrams(cat)
		idx.catTrigrams[cat] = make(map[string]struct{}, len(grams))
		for _, g := range grams {
			idx.catTrigrams[cat][g] = struct{}{}
		}
	}

	idx.sortedNames = make([]nameEntry, n)
	for i, p := range products {
		idx.sortedNames[i] = nameEntry{Name: strings.ToLower(p.Name), ID: i}
	}
	slices.SortFunc(idx.sortedNames, func(a, b nameEntry) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return idx
}

// Search returns products matching query, scored by Jaccard similarity.
// For queries shorter than 3 characters, it falls back to prefix matching.
func (idx *Index) Search(query string) []SearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	if len(query) < 3 {
		return idx.prefixSearch(query)
	}
	queryGrams := ExtractTrigrams(query)
	if len(queryGrams) == 0 {
		return nil
	}
	return idx.searchWithGrams(queryGrams)
}

// SearchWithGrams performs a Jaccard trigram search using pre-extracted trigrams.
// Use this to avoid redundant ExtractTrigrams calls when trigrams are already available.
func (idx *Index) SearchWithGrams(queryGrams []string) []SearchResult {
	if len(queryGrams) == 0 {
		return nil
	}
	return idx.searchWithGrams(queryGrams)
}

// gramEntry pairs a trigram with its posting list size for rarity-first sorting.
type gramEntry struct {
	gram string
	size int
}

// searchWithGrams is the core Jaccard search implementation.
// It processes trigrams from rarest to most common posting list,
// caps candidates at maxCandidates, and only scores those with enough
// trigram overlap to avoid wasted work on large catalogs.
func (idx *Index) searchWithGrams(queryGrams []string) []SearchResult {
	// Deduplicate query trigrams in-place (sort + compact, avoids map alloc).
	slices.Sort(queryGrams)
	queryGrams = slices.Compact(queryGrams)
	numQueryGrams := len(queryGrams)

	// Sort by posting list size (rarest first). Stack-allocate for typical queries.
	var sortBuf [16]gramEntry
	sorted := sortBuf[:0]
	for _, g := range queryGrams {
		sorted = append(sorted, gramEntry{g, len(idx.posting[g])})
	}
	slices.SortFunc(sorted, func(a, b gramEntry) int {
		return cmp.Compare(a.size, b.size)
	})

	// Count trigram hits per candidate using a pooled array.
	hitsPtr := idx.hitsPool.Get().(*[]uint8)
	hits := *hitsPtr

	const (
		maxPostingExpand = 200 // only add new candidates from small posting lists
		maxCandidates    = 500 // stop expanding after this many candidates
	)

	// Stack-allocate touched buffer for the common case (< 256 candidates).
	var touchBuf [256]int
	touched := touchBuf[:0]
	for _, id := range idx.posting[sorted[0].gram] {
		if hits[id] < 255 {
			hits[id]++
		}
		touched = append(touched, id)
	}
	for _, entry := range sorted[1:] {
		posting := idx.posting[entry.gram]
		expand := len(posting) <= maxPostingExpand && len(touched) < maxCandidates
		for _, id := range posting {
			if hits[id] > 0 {
				if hits[id] < 255 {
					hits[id]++
				}
			} else if expand {
				hits[id]++
				touched = append(touched, id)
			}
		}
	}

	// Minimum hit threshold: require >= 1/3 of query trigrams to match.
	minHits := uint8(min(255, max(1, numQueryGrams/3)))

	// Score candidates that pass the threshold.
	var results []SearchResult
	for _, id := range touched {
		h := hits[id]
		if h < minHits {
			continue
		}
		intersection := int(h)
		unionSize := numQueryGrams + idx.docGramCounts[id] - intersection
		if unionSize <= 0 {
			continue
		}
		results = append(results, SearchResult{
			ProductID: id,
			Score:     float64(intersection) / float64(unionSize),
		})
	}

	// Clear touched entries and return the hit array to the pool.
	for _, id := range touched {
		hits[id] = 0
	}
	idx.hitsPool.Put(hitsPtr)

	return results
}

// prefixSearch uses binary search on the sorted name array to find products
// whose lowercased name starts with the given short query.
func (idx *Index) prefixSearch(query string) []SearchResult {
	lo, _ := slices.BinarySearchFunc(idx.sortedNames, query, func(entry nameEntry, target string) int {
		return cmp.Compare(entry.Name, target)
	})

	var results []SearchResult
	for i := lo; i < len(idx.sortedNames); i++ {
		if !strings.HasPrefix(idx.sortedNames[i].Name, query) {
			break
		}
		results = append(results, SearchResult{
			ProductID: idx.sortedNames[i].ID,
			Score:     1.0,
		})
	}
	return results
}

// SearchCategories returns category names matching query, ranked by
// Jaccard similarity of their trigrams. Only categories with a positive
// score are returned.
func (idx *Index) SearchCategories(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	queryGrams := ExtractTrigrams(query)
	if len(queryGrams) == 0 {
		var matches []string
		for cat := range idx.catProducts {
			if strings.HasPrefix(cat, query) {
				matches = append(matches, cat)
			}
		}
		return matches
	}

	querySet := make(map[string]struct{}, len(queryGrams))
	for _, g := range queryGrams {
		querySet[g] = struct{}{}
	}

	type scored struct {
		name  string
		score float64
	}
	var results []scored

	for cat, catGrams := range idx.catTrigrams {
		intersection := 0
		for g := range querySet {
			if _, ok := catGrams[g]; ok {
				intersection++
			}
		}
		if intersection == 0 {
			continue
		}
		unionSize := len(querySet) + len(catGrams) - intersection
		results = append(results, scored{
			name:  cat,
			score: float64(intersection) / float64(unionSize),
		})
	}

	slices.SortFunc(results, func(a, b scored) int {
		return cmp.Compare(b.score, a.score) // descending
	})

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.name
	}
	return names
}

// BestCategory returns the single best-matching category for query.
func (idx *Index) BestCategory(query string) (string, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return "", false
	}

	queryGrams := ExtractTrigrams(query)
	if len(queryGrams) == 0 {
		best := ""
		for cat := range idx.catProducts {
			if strings.HasPrefix(cat, query) && (best == "" || cat < best) {
				best = cat
			}
		}
		return best, best != ""
	}

	return idx.bestCategoryWithGrams(queryGrams), true
}

// BestCategoryWithGrams returns the best-matching category using pre-extracted trigrams.
func (idx *Index) BestCategoryWithGrams(queryGrams []string) (string, bool) {
	if len(queryGrams) == 0 {
		return "", false
	}
	best := idx.bestCategoryWithGrams(queryGrams)
	return best, best != ""
}

func (idx *Index) bestCategoryWithGrams(queryGrams []string) string {
	bestName := ""
	bestScore := 0.0
	numQueryGrams := len(queryGrams)

	for cat, catGrams := range idx.catTrigrams {
		intersection := 0
		for _, g := range queryGrams {
			if _, ok := catGrams[g]; ok {
				intersection++
			}
		}
		if intersection == 0 {
			continue
		}
		unionSize := numQueryGrams + len(catGrams) - intersection
		score := float64(intersection) / float64(unionSize)
		if score > bestScore || (score == bestScore && (bestName == "" || cat < bestName)) {
			bestName = cat
			bestScore = score
		}
	}
	return bestName
}

// ProductsByCategory returns the product IDs belonging to the given category.
func (idx *Index) ProductsByCategory(category string) []int {
	return idx.catProducts[category]
}

// CategoryNames returns an iterator over all category names.
func (idx *Index) CategoryNames() iter.Seq[string] {
	return maps.Keys(idx.catProducts)
}
