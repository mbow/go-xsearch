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
	"maps"
	"github.com/mbow/go-xsearch/catalog"
	"slices"
	"sort"
	"strings"
	"sync"
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
	products    []catalog.Product
	posting     map[string][]int              // trigram → product IDs
	trigrams    map[int]map[string]struct{}   // product ID → its trigram set
	catTrigrams map[string]map[string]struct{} // category → its trigram set
	catProducts map[string][]int              // category → product IDs
	sortedNames []nameEntry                   // sorted by lowercased name for binary search
	hitsPool    sync.Pool                     // reusable []uint8 hit counters
}

// Snapshot holds the serializable state of an [Index].
type Snapshot struct {
	Posting     map[string][]int    `cbor:"posting"`
	Trigrams    map[int][]string    `cbor:"trigrams"`
	CatTrigrams map[string][]string `cbor:"cat_trigrams"`
	CatProducts map[string][]int   `cbor:"cat_products"`
	SortedNames []nameEntry        `cbor:"sorted_names"`
}

// ToSnapshot exports the index state for serialization.
func (idx *Index) ToSnapshot() Snapshot {
	tris := make(map[int][]string, len(idx.trigrams))
	for id, grams := range idx.trigrams {
		tris[id] = slices.Collect(maps.Keys(grams))
	}

	catTris := make(map[string][]string, len(idx.catTrigrams))
	for cat, grams := range idx.catTrigrams {
		catTris[cat] = slices.Collect(maps.Keys(grams))
	}

	return Snapshot{
		Posting:     idx.posting,
		Trigrams:    tris,
		CatTrigrams: catTris,
		CatProducts: idx.catProducts,
		SortedNames: idx.sortedNames,
	}
}

// FromSnapshot restores an [Index] from a serialized [Snapshot] and product list.
func FromSnapshot(s Snapshot, products []catalog.Product) *Index {
	trigrams := make(map[int]map[string]struct{}, len(s.Trigrams))
	for id, grams := range s.Trigrams {
		m := make(map[string]struct{}, len(grams))
		for _, g := range grams {
			m[g] = struct{}{}
		}
		trigrams[id] = m
	}

	catTrigrams := make(map[string]map[string]struct{}, len(s.CatTrigrams))
	for cat, grams := range s.CatTrigrams {
		m := make(map[string]struct{}, len(grams))
		for _, g := range grams {
			m[g] = struct{}{}
		}
		catTrigrams[cat] = m
	}

	n := len(products)
	return &Index{
		products:    products,
		posting:     s.Posting,
		trigrams:    trigrams,
		catTrigrams: catTrigrams,
		catProducts: s.CatProducts,
		sortedNames: s.SortedNames,
		hitsPool:    sync.Pool{New: func() any { s := make([]uint8, n); return &s }},
	}
}

// NewIndex builds an n-gram inverted index from the given products.
// Each product's name and tags are tokenized into trigrams and added
// to the inverted posting lists.
func NewIndex(products []catalog.Product) *Index {
	n := len(products)
	idx := &Index{
		products:    products,
		posting:     make(map[string][]int),
		trigrams:    make(map[int]map[string]struct{}),
		catTrigrams: make(map[string]map[string]struct{}),
		catProducts: make(map[string][]int),
		hitsPool:    sync.Pool{New: func() any { s := make([]uint8, n); return &s }},
	}

	for id, p := range products {
		grams := ExtractTrigrams(p.Name)
		for _, tag := range p.Tags {
			grams = append(grams, ExtractTrigrams(tag)...)
		}
		idx.trigrams[id] = make(map[string]struct{}, len(grams))
		for _, g := range grams {
			idx.trigrams[id][g] = struct{}{}
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
//
// The algorithm processes trigrams from rarest to most common posting list,
// caps candidates at [maxCandidates], and only scores those with enough
// trigram overlap to avoid wasted work on large catalogs.
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

	// Deduplicate query trigrams.
	querySet := make(map[string]struct{}, len(queryGrams))
	for _, g := range queryGrams {
		querySet[g] = struct{}{}
	}
	numQueryGrams := len(querySet)

	// Sort query trigrams by posting list size (rarest first).
	type gramEntry struct {
		gram string
		size int
	}
	sorted := make([]gramEntry, 0, numQueryGrams)
	for g := range querySet {
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

	var touched []int
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

	// Minimum hit threshold: require ≥ 1/3 of query trigrams to match.
	minHits := uint8(max(1, numQueryGrams/3))

	// Score candidates that pass the threshold.
	var results []SearchResult
	for _, id := range touched {
		h := hits[id]
		if h < minHits {
			continue
		}
		intersection := int(h)
		unionSize := numQueryGrams + len(idx.trigrams[id]) - intersection
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
	n := len(idx.sortedNames)
	lo := sort.Search(n, func(i int) bool {
		return idx.sortedNames[i].Name >= query
	})

	var results []SearchResult
	for i := lo; i < n; i++ {
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

// ProductsByCategory returns the product IDs belonging to the given category.
func (idx *Index) ProductsByCategory(category string) []int {
	return idx.catProducts[category]
}

// CategoryNames returns the category-to-product-IDs map, allowing callers
// to iterate category names without copying.
func (idx *Index) CategoryNames() map[string][]int {
	return idx.catProducts
}
