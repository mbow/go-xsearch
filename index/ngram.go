package index

import (
	"search/catalog"
	"sort"
	"strings"
)

// ExtractTrigrams returns all overlapping 3-character substrings
// from the normalized (lowercased, trimmed) input.
// Returns nil if input has fewer than 3 characters after normalization.
func ExtractTrigrams(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 {
		return nil
	}
	trigrams := make([]string, 0, len(s)-2)
	for i := 0; i <= len(s)-3; i++ {
		trigrams = append(trigrams, s[i:i+3])
	}
	return trigrams
}

// SearchResult holds a product match with its Jaccard similarity score.
type SearchResult struct {
	ProductID int
	Score     float64
}

// bitset is a compact set of product IDs backed by a uint64 slice.
// Set/test operations are single CPU instructions instead of map hash+lookup.
type bitset struct {
	words []uint64
	size  int
}

func newBitset(n int) bitset {
	return bitset{
		words: make([]uint64, (n+63)/64),
		size:  n,
	}
}

func (b *bitset) set(i int)      { b.words[i/64] |= 1 << (uint(i) % 64) }
func (b *bitset) test(i int) bool { return b.words[i/64]&(1<<(uint(i)%64)) != 0 }

// forEach calls fn for each set bit.
func (b *bitset) forEach(fn func(int)) {
	for w := range b.words {
		bits := b.words[w]
		base := w * 64
		for bits != 0 {
			tz := bits & -bits       // isolate lowest set bit
			pos := base + popcount64minus1(tz)
			fn(pos)
			bits &= bits - 1 // clear lowest set bit
		}
	}
}

// popcount64minus1 returns the bit position of a single set bit (power of 2).
func popcount64minus1(v uint64) int {
	// de Bruijn sequence for bit position lookup
	pos := 0
	for v >>= 1; v != 0; v >>= 1 {
		pos++
	}
	return pos
}

// Index is an n-gram inverted index over a product catalog.
type Index struct {
	products    []catalog.Product
	posting     map[string][]int            // trigram -> list of product IDs
	trigrams    map[int]map[string]struct{} // product ID -> set of its trigrams
	catTrigrams map[string]map[string]struct{} // category name -> set of its trigrams
	catProducts map[string][]int               // category name -> list of product IDs
}

// NewIndex builds an n-gram inverted index from the given products.
func NewIndex(products []catalog.Product) *Index {
	idx := &Index{
		products:    products,
		posting:     make(map[string][]int),
		trigrams:    make(map[int]map[string]struct{}),
		catTrigrams: make(map[string]map[string]struct{}),
		catProducts: make(map[string][]int),
	}

	for id, p := range products {
		grams := ExtractTrigrams(p.Name)
		idx.trigrams[id] = make(map[string]struct{}, len(grams))
		for _, g := range grams {
			idx.trigrams[id][g] = struct{}{}
			idx.posting[g] = append(idx.posting[g], id)
		}
		idx.catProducts[p.Category] = append(idx.catProducts[p.Category], id)
	}

	// Build category trigrams
	for cat := range idx.catProducts {
		grams := ExtractTrigrams(cat)
		idx.catTrigrams[cat] = make(map[string]struct{}, len(grams))
		for _, g := range grams {
			idx.catTrigrams[cat][g] = struct{}{}
		}
	}

	return idx
}

// Search returns products matching the query, scored by Jaccard similarity.
// For queries shorter than 3 characters, falls back to prefix matching.
func (idx *Index) Search(query string) []SearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	// Short query: prefix fallback
	if len(query) < 3 {
		return idx.prefixSearch(query)
	}

	queryGrams := ExtractTrigrams(query)
	if len(queryGrams) == 0 {
		return nil
	}

	// Build set of query trigrams
	querySet := make(map[string]struct{}, len(queryGrams))
	for _, g := range queryGrams {
		querySet[g] = struct{}{}
	}

	// Union of all posting lists using bitset (single CPU instruction per set/test)
	candidates := newBitset(len(idx.products))
	for _, g := range queryGrams {
		for _, id := range idx.posting[g] {
			candidates.set(id)
		}
	}

	// Score each candidate by Jaccard similarity
	var results []SearchResult
	candidates.forEach(func(id int) {
		productGrams := idx.trigrams[id]

		// Intersection size
		intersection := 0
		for g := range querySet {
			if _, ok := productGrams[g]; ok {
				intersection++
			}
		}

		// Union size
		unionSize := len(querySet) + len(productGrams) - intersection

		score := float64(intersection) / float64(unionSize)
		results = append(results, SearchResult{ProductID: id, Score: score})
	})

	return results
}

// prefixSearch does a linear scan for products whose lowercase name
// starts with the given short query.
func (idx *Index) prefixSearch(query string) []SearchResult {
	var results []SearchResult
	for id, p := range idx.products {
		if strings.HasPrefix(strings.ToLower(p.Name), query) {
			results = append(results, SearchResult{ProductID: id, Score: 1.0})
		}
	}
	return results
}

// SearchCategories returns category names that match the query,
// ranked by Jaccard similarity of their trigrams. Only returns
// categories with a score above 0.
func (idx *Index) SearchCategories(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	queryGrams := ExtractTrigrams(query)
	// For short queries, prefix match on category names
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
		score := float64(intersection) / float64(unionSize)
		results = append(results, scored{name: cat, score: score})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.name
	}
	return names
}

// ProductsByCategory returns product IDs belonging to the given category.
func (idx *Index) ProductsByCategory(category string) []int {
	return idx.catProducts[category]
}
