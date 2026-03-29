package index

import (
	"search/catalog"
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

// Index is an n-gram inverted index over a product catalog.
type Index struct {
	products []catalog.Product
	posting  map[string][]int            // trigram -> list of product IDs
	trigrams map[int]map[string]struct{} // product ID -> set of its trigrams
}

// NewIndex builds an n-gram inverted index from the given products.
func NewIndex(products []catalog.Product) *Index {
	idx := &Index{
		products: products,
		posting:  make(map[string][]int),
		trigrams: make(map[int]map[string]struct{}),
	}

	for id, p := range products {
		grams := ExtractTrigrams(p.Name)
		idx.trigrams[id] = make(map[string]struct{}, len(grams))
		for _, g := range grams {
			idx.trigrams[id][g] = struct{}{}
			idx.posting[g] = append(idx.posting[g], id)
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

	// Union of all posting lists
	candidates := make(map[int]struct{})
	for _, g := range queryGrams {
		for _, id := range idx.posting[g] {
			candidates[id] = struct{}{}
		}
	}

	// Score each candidate by Jaccard similarity
	results := make([]SearchResult, 0, len(candidates))
	for id := range candidates {
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
	}

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
