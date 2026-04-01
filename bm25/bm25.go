// Package bm25 implements BM25 scoring with prefix boosting for autocomplete search.
//
// It builds an inverted index from a product catalog and scores query-document
// pairs using the Okapi BM25 formula. Prefix matching on product names provides
// an additional ranking boost so that direct name matches (e.g., "bud" → "Budweiser")
// rank above incidental substring matches (e.g., "bud" in "Funky Buddha").
package bm25

import (
	"container/heap"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
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

const maxSearchResults = 10

type resultHeap []SearchResult

func (h resultHeap) Len() int            { return len(h) }
func (h resultHeap) Less(i, j int) bool {
	if h[i].Score != h[j].Score {
		return h[i].Score < h[j].Score
	}
	return h[i].ProductID > h[j].ProductID // higher ID = lower priority (evicted first)
}
func (h resultHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *resultHeap) Push(x any)         { *h = append(*h, x.(SearchResult)) }
func (h *resultHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// Index holds the precomputed BM25 data structures for a product catalog.
type Index struct {
	idf            map[string]float64
	termFreqs      []map[string]int
	docLens        []int
	avgDocLen      float64
	wordPrefixes   []map[string]struct{}   // product ID → set of word prefixes
	prefixPosting  map[string][]int        // prefix → product IDs (inverted index for fast prefix lookup)
	posting        map[string][]int
	seenPool       sync.Pool
	k1             float64
	b              float64
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
		idf:           make(map[string]float64),
		termFreqs:     make([]map[string]int, n),
		docLens:       make([]int, n),
		wordPrefixes:  make([]map[string]struct{}, n),
		prefixPosting: make(map[string][]int),
		posting:       make(map[string][]int),
		k1:            DefaultK1,
		b:             DefaultB,
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
			maxPfx := min(len(runes), 6)
			for length := 1; length <= maxPfx; length++ {
				prefixes[string(runes[:length])] = struct{}{}
			}
		}
		idx.wordPrefixes[i] = prefixes

		// Build prefix inverted index for O(1) prefix lookup at query time.
		for pfx := range prefixes {
			idx.prefixPosting[pfx] = append(idx.prefixPosting[pfx], i)
		}
	}

	// Average document length.
	if n > 0 {
		idx.avgDocLen = float64(totalLen) / float64(n)
	}

	// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
	for term, d := range df {
		idx.idf[term] = math.Log((float64(n)-float64(d)+0.5)/(float64(d)+0.5) + 1)
	}

	idx.seenPool = sync.Pool{New: func() any { s := make([]bool, n); return &s }}

	return idx
}

// Score computes the BM25 score for a single product against the given query terms.
// productID must be in [0, len(products)) or Score returns 0.
func (idx *Index) Score(productID int, queryTerms []string) float64 {
	if productID < 0 || productID >= len(idx.termFreqs) {
		return 0
	}
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
// productID must be in [0, len(products)) or HasPrefixMatch returns false.
func (idx *Index) HasPrefixMatch(productID int, queryTerms []string) bool {
	if productID < 0 || productID >= len(idx.wordPrefixes) {
		return false
	}
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

	// Collect candidates from term posting lists and prefix posting lists.
	seenPtr := idx.seenPool.Get().(*[]bool)
	seen := *seenPtr
	var candidates []int

	for _, term := range queryTerms {
		for _, id := range idx.posting[term] {
			if !seen[id] {
				seen[id] = true
				candidates = append(candidates, id)
			}
		}
		for _, id := range idx.prefixPosting[term] {
			if !seen[id] {
				seen[id] = true
				candidates = append(candidates, id)
			}
		}
	}

	if len(candidates) == 0 {
		idx.seenPool.Put(seenPtr)
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

	// Score each candidate using a min-heap of size maxSearchResults.
	h := make(resultHeap, 0, maxSearchResults+1)

	for _, id := range candidates {
		score := idx.Score(id, queryTerms)
		pm := idx.HasPrefixMatch(id, queryTerms)
		if pm {
			score += prefixBonus
		}
		if score <= 0 {
			continue
		}

		if h.Len() < maxSearchResults {
			heap.Push(&h, SearchResult{ProductID: id, Score: score, PrefixMatch: pm})
		} else if score > h[0].Score {
			h[0] = SearchResult{ProductID: id, Score: score, PrefixMatch: pm}
			heap.Fix(&h, 0)
		}
	}

	// Clear seen entries and return to pool.
	for _, id := range candidates {
		seen[id] = false
	}
	idx.seenPool.Put(seenPtr)

	// Drain heap in descending score order.
	results := make([]SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(&h).(SearchResult)
	}

	return results
}

// Snapshot is a serializable representation of an Index, suitable for CBOR encoding.
type Snapshot struct {
	IDF           map[string]float64 `cbor:"idf"`
	TermFreqs     []map[string]int   `cbor:"term_freqs"`
	DocLens       []int              `cbor:"doc_lens"`
	AvgDocLen     float64            `cbor:"avg_doc_len"`
	WordPrefixes  [][]string         `cbor:"word_prefixes"`
	PrefixPosting map[string][]int   `cbor:"prefix_posting"`
	Posting       map[string][]int   `cbor:"posting"`
	K1            float64            `cbor:"k1"`
	B             float64            `cbor:"b"`
}

// ToSnapshot converts the Index into a Snapshot for serialization.
// Word prefix sets are converted to sorted string slices.
func (idx *Index) ToSnapshot() Snapshot {
	wp := make([][]string, len(idx.wordPrefixes))
	for i, m := range idx.wordPrefixes {
		s := make([]string, 0, len(m))
		for k := range m {
			s = append(s, k)
		}
		slices.Sort(s)
		wp[i] = s
	}
	return Snapshot{
		IDF:           idx.idf,
		TermFreqs:     idx.termFreqs,
		DocLens:       idx.docLens,
		AvgDocLen:     idx.avgDocLen,
		WordPrefixes:  wp,
		PrefixPosting: idx.prefixPosting,
		Posting:       idx.posting,
		K1:            idx.k1,
		B:             idx.b,
	}
}

// FromSnapshot restores an Index from a Snapshot.
// Word prefix string slices are converted back to sets.
// Returns an error if the snapshot has inconsistent slice lengths.
func FromSnapshot(s Snapshot) (*Index, error) {
	n := len(s.TermFreqs)
	if len(s.DocLens) != n || len(s.WordPrefixes) != n {
		return nil, fmt.Errorf("bm25: snapshot length mismatch: termFreqs=%d, docLens=%d, wordPrefixes=%d",
			n, len(s.DocLens), len(s.WordPrefixes))
	}

	wp := make([]map[string]struct{}, n)
	for i, list := range s.WordPrefixes {
		m := make(map[string]struct{}, len(list))
		for _, k := range list {
			m[k] = struct{}{}
		}
		wp[i] = m
	}
	idx := &Index{
		idf:           s.IDF,
		termFreqs:     s.TermFreqs,
		docLens:       s.DocLens,
		avgDocLen:     s.AvgDocLen,
		wordPrefixes:  wp,
		prefixPosting: s.PrefixPosting,
		posting:       s.Posting,
		k1:            s.K1,
		b:             s.B,
	}
	idx.seenPool = sync.Pool{New: func() any { s := make([]bool, n); return &s }}
	return idx, nil
}
