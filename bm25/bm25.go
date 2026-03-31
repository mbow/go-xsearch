// Package bm25 implements BM25 scoring with prefix boosting for autocomplete search.
//
// It builds an inverted index from a product catalog and scores query-document
// pairs using the Okapi BM25 formula. Prefix matching on product names provides
// an additional ranking boost so that direct name matches (e.g., "bud" → "Budweiser")
// rank above incidental substring matches (e.g., "bud" in "Funky Buddha").
package bm25

import (
	"strings"
	"unicode"
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
