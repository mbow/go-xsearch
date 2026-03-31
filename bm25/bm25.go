// Package bm25 implements BM25 scoring with prefix boosting for autocomplete search.
//
// It builds an inverted index from a product catalog and scores query-document
// pairs using the Okapi BM25 formula. Prefix matching on product names provides
// an additional ranking boost so that direct name matches (e.g., "bud" → "Budweiser")
// rank above incidental substring matches (e.g., "bud" in "Funky Buddha").
package bm25

import (
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/mbow/go-xsearch/catalog"
)

const (
	DefaultK1 = 1.2
	DefaultB  = 0.75
)

// SearchResult holds a single scored search hit.
type SearchResult struct {
	ProductID   int
	Score       float64
	PrefixMatch bool
}

// Index holds the precomputed BM25 data structures for a product catalog.
type Index struct {
	idf          map[string]float64
	termFreqs    []map[string]int
	docLens      []int
	avgDocLen    float64
	wordPrefixes []map[string]struct{}
	posting      map[string][]int
	k1           float64
	b            float64
}

// Tokenize splits s on whitespace, lowercases each token, and filters out
// pure-punctuation tokens (keeps tokens with at least one alphanumeric char).
func Tokenize(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		lower := strings.ToLower(f)
		if hasAlphanumeric(lower) {
			tokens = append(tokens, lower)
		}
	}
	if len(tokens) == 0 {
		return nil
	}
	return tokens
}

// hasAlphanumeric reports whether s contains at least one alphanumeric character.
func hasAlphanumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// NewIndex builds a BM25 index from the given product catalog.
//
// It tokenizes each product's Name, Category, and Tags into a single document,
// computes term frequencies, document lengths, posting lists, IDF values,
// and word prefixes (from product names only).
func NewIndex(products []catalog.Product) *Index {
	n := len(products)
	idx := &Index{
		idf:          make(map[string]float64),
		termFreqs:    make([]map[string]int, n),
		docLens:      make([]int, n),
		wordPrefixes: make([]map[string]struct{}, n),
		posting:      make(map[string][]int),
		k1:           DefaultK1,
		b:            DefaultB,
	}

	// df tracks how many documents contain each term.
	df := make(map[string]int)
	totalLen := 0

	for i, p := range products {
		// Tokenize full document: Name + Category + Tags.
		doc := p.Name + " " + p.Category
		if len(p.Tags) > 0 {
			doc += " " + strings.Join(p.Tags, " ")
		}
		tokens := Tokenize(doc)

		// Term frequencies for this document.
		tf := make(map[string]int, len(tokens))
		for _, tok := range tokens {
			tf[tok]++
		}
		idx.termFreqs[i] = tf
		idx.docLens[i] = len(tokens)
		totalLen += len(tokens)

		// Posting lists and document frequency.
		for term := range tf {
			idx.posting[term] = append(idx.posting[term], i)
			df[term]++
		}

		// Word prefixes from product NAME only.
		nameTokens := Tokenize(p.Name)
		prefixes := make(map[string]struct{})
		for _, word := range nameTokens {
			runes := []rune(word)
			for length := 1; length <= len(runes); length++ {
				prefixes[string(runes[:length])] = struct{}{}
			}
		}
		idx.wordPrefixes[i] = prefixes
	}

	// Average document length.
	if n > 0 {
		idx.avgDocLen = float64(totalLen) / float64(n)
	}

	// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
	for term, d := range df {
		idx.idf[term] = math.Log((float64(n)-float64(d)+0.5)/(float64(d)+0.5) + 1)
	}

	return idx
}

// Score computes the BM25 score for a single product against the given query terms.
func (idx *Index) Score(productID int, queryTerms []string) float64 {
	tf := idx.termFreqs[productID]
	dl := float64(idx.docLens[productID])
	var score float64
	for _, term := range queryTerms {
		idf, ok := idx.idf[term]
		if !ok {
			continue
		}
		f := float64(tf[term])
		if f == 0 {
			continue
		}
		score += idf * (f * (idx.k1 + 1)) / (f + idx.k1*(1-idx.b+idx.b*dl/idx.avgDocLen))
	}
	return score
}

// HasPrefixMatch reports whether any query term exists in the product's
// word-prefix set (built from product name words).
func (idx *Index) HasPrefixMatch(productID int, queryTerms []string) bool {
	prefixes := idx.wordPrefixes[productID]
	for _, term := range queryTerms {
		if _, ok := prefixes[term]; ok {
			return true
		}
	}
	return false
}

// Search performs a full BM25 search with prefix boosting and returns results
// sorted by descending score.
func (idx *Index) Search(query string) []SearchResult {
	queryTerms := Tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// Collect candidates from posting lists.
	seen := make(map[int]struct{})
	for _, term := range queryTerms {
		for _, id := range idx.posting[term] {
			seen[id] = struct{}{}
		}
	}

	// Also collect candidates via prefix matching (scan all products).
	// If posting lists found candidates, prefix matches still contribute
	// additional candidates that receive a prefix bonus score.
	for id := range idx.wordPrefixes {
		if idx.HasPrefixMatch(id, queryTerms) {
			seen[id] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	// Compute maxIDF across query terms (default 1.0 if none found).
	maxIDF := 1.0
	for _, term := range queryTerms {
		if idf, ok := idx.idf[term]; ok && idf > maxIDF {
			maxIDF = idf
		}
	}
	prefixBonus := 0.5 * maxIDF

	// Score each candidate.
	results := make([]SearchResult, 0, len(seen))
	for id := range seen {
		score := idx.Score(id, queryTerms)
		pm := idx.HasPrefixMatch(id, queryTerms)
		if pm {
			score += prefixBonus
		}
		if score > 0 {
			results = append(results, SearchResult{
				ProductID:   id,
				Score:       score,
				PrefixMatch: pm,
			})
		}
	}

	// Sort by score descending.
	slices.SortFunc(results, func(a, b SearchResult) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return 0
	})

	return results
}
