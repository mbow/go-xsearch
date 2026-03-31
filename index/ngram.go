package index

import (
	"search/catalog"
	"slices"
	"sort"
	"strings"
	"sync"
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
	hitsPool    sync.Pool                      // reusable []int16 hit counters
}

// Snapshot holds the serializable state of an index.
type Snapshot struct {
	Posting     map[string][]int            `cbor:"posting"`
	Trigrams    map[int][]string            `cbor:"trigrams"`     // flattened from map[int]map[string]struct{}
	CatTrigrams map[string][]string         `cbor:"cat_trigrams"` // flattened
	CatProducts map[string][]int            `cbor:"cat_products"`
}

// ToSnapshot exports the index state for serialization.
func (idx *Index) ToSnapshot() Snapshot {
	tris := make(map[int][]string, len(idx.trigrams))
	for id, grams := range idx.trigrams {
		s := make([]string, 0, len(grams))
		for g := range grams {
			s = append(s, g)
		}
		tris[id] = s
	}

	catTris := make(map[string][]string, len(idx.catTrigrams))
	for cat, grams := range idx.catTrigrams {
		s := make([]string, 0, len(grams))
		for g := range grams {
			s = append(s, g)
		}
		catTris[cat] = s
	}

	return Snapshot{
		Posting:     idx.posting,
		Trigrams:    tris,
		CatTrigrams: catTris,
		CatProducts: idx.catProducts,
	}
}

// FromSnapshot restores an index from serialized state plus the product list.
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
		hitsPool:    sync.Pool{New: func() any { s := make([]int16, n); return &s }},
	}
}

// NewIndex builds an n-gram inverted index from the given products.
func NewIndex(products []catalog.Product) *Index {
	n := len(products)
	idx := &Index{
		products:    products,
		posting:     make(map[string][]int),
		trigrams:    make(map[int]map[string]struct{}),
		catTrigrams: make(map[string]map[string]struct{}),
		catProducts: make(map[string][]int),
		hitsPool:    sync.Pool{New: func() any { s := make([]int16, n); return &s }},
	}

	for id, p := range products {
		grams := ExtractTrigrams(p.Name)
		// Also index tags — each tag's trigrams become searchable
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

	// Build set of unique query trigrams
	querySet := make(map[string]struct{}, len(queryGrams))
	for _, g := range queryGrams {
		querySet[g] = struct{}{}
	}
	numQueryGrams := len(querySet)

	// Sort query trigrams by posting list size (rarest first).
	// This dramatically reduces work: we seed candidates from the smallest
	// posting list, then only check those candidates against larger lists.
	type gramEntry struct {
		gram string
		size int
	}
	sorted := make([]gramEntry, 0, numQueryGrams)
	for g := range querySet {
		sorted = append(sorted, gramEntry{g, len(idx.posting[g])})
	}
	slices.SortFunc(sorted, func(a, b gramEntry) int {
		return a.size - b.size
	})

	// Count trigram hits per candidate product using pooled array.
	hitsPtr := idx.hitsPool.Get().(*[]int16)
	hits := *hitsPtr
	// Seed candidates from the rarest trigram, then expand only from
	// small posting lists. Large posting lists only update existing candidates.
	const maxPostingExpand = 200 // only add new candidates from small posting lists
	const maxCandidates = 500   // stop expanding candidates after this many
	var touched []int
	for _, id := range idx.posting[sorted[0].gram] {
		hits[id]++
		touched = append(touched, id)
	}
	for i := 1; i < len(sorted); i++ {
		posting := idx.posting[sorted[i].gram]
		expand := len(posting) <= maxPostingExpand && len(touched) < maxCandidates
		for _, id := range posting {
			if hits[id] > 0 {
				hits[id]++
			} else if expand {
				hits[id]++
				touched = append(touched, id)
			}
		}
	}

	// Minimum hit threshold: at least 1/3 of query trigrams must match.
	// This filters out noise from common trigrams at scale.
	minHits := int16(1)
	if numQueryGrams > 3 {
		minHits = int16(numQueryGrams / 3)
	}

	// Score only candidates that pass the hit threshold
	var results []SearchResult
	for _, id := range touched {
		h := hits[id]
		if h < minHits {
			continue
		}
		productGrams := idx.trigrams[id]

		intersection := int(h)
		unionSize := numQueryGrams + len(productGrams) - intersection

		score := float64(intersection) / float64(unionSize)
		results = append(results, SearchResult{ProductID: id, Score: score})
	}

	// Clear only touched entries and return to pool
	for _, id := range touched {
		hits[id] = 0
	}
	idx.hitsPool.Put(hitsPtr)

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
