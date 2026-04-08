package xsearch

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"sync"
)

type termFreq struct {
	Term  string `cbor:"t"`
	Count int    `cbor:"c"`
}

type bm25FieldIndex struct {
	idf        map[string]float64
	termFreqs  [][]termFreq
	docLens    []int
	docWeights []float64
	avgDocLen  float64
	posting    map[string][]int
}

type bm25Index struct {
	fields         map[string]*bm25FieldIndex
	prefixPosting  map[string][]int
	primaryWeights []float64
	k1             float64
	b              float64
	seenPool       sync.Pool
}

type bm25FieldSnapshot struct {
	IDF        map[string]float64 `cbor:"idf"`
	TermFreqs  [][]termFreq       `cbor:"term_freqs"`
	DocLens    []int              `cbor:"doc_lens"`
	DocWeights []float64          `cbor:"doc_weights"`
	AvgDocLen  float64            `cbor:"avg_doc_len"`
	Posting    map[string][]int   `cbor:"posting"`
}

type bm25Snapshot struct {
	Fields         map[string]bm25FieldSnapshot `cbor:"fields"`
	PrefixPosting  map[string][]int             `cbor:"prefix_posting"`
	PrimaryWeights []float64                    `cbor:"primary_weights"`
	K1             float64                      `cbor:"k1"`
	B              float64                      `cbor:"b"`
}

type bm25SearchResult struct {
	Doc         int
	Score       float64
	PrefixMatch bool
}

func newBM25Index(items []preparedItem, cfg engineConfig) *bm25Index {
	n := len(items)
	idx := &bm25Index{
		fields:         make(map[string]*bm25FieldIndex),
		prefixPosting:  make(map[string][]int),
		primaryWeights: make([]float64, n),
		k1:             cfg.bm25K1,
		b:              cfg.bm25B,
	}
	idx.seenPool.New = func() any {
		s := make([]uint8, n)
		return &s
	}

	type stats struct {
		df       map[string]int
		totalLen int
		docs     int
	}
	fieldStats := make(map[string]*stats)

	for docID, item := range items {
		primary := item.Fields[item.primaryField]
		idx.primaryWeights[docID] = primary.Weight

		prefixSet := make(map[string]struct{})
		for _, value := range primary.Values {
			for _, word := range tokenize(value) {
				runes := []rune(word)
				maxPfx := min(len(runes), 6)
				for length := 1; length <= maxPfx; length++ {
					prefixSet[string(runes[:length])] = struct{}{}
				}
			}
		}
		for prefix := range prefixSet {
			idx.prefixPosting[prefix] = append(idx.prefixPosting[prefix], docID)
		}

		for _, field := range item.Fields {
			fi, ok := idx.fields[field.Name]
			if !ok {
				fi = &bm25FieldIndex{
					idf:        make(map[string]float64),
					termFreqs:  make([][]termFreq, n),
					docLens:    make([]int, n),
					docWeights: make([]float64, n),
					posting:    make(map[string][]int),
				}
				idx.fields[field.Name] = fi
				fieldStats[field.Name] = &stats{df: make(map[string]int)}
			}

			doc := strings.Join(field.Values, " ")
			tokens := tokenize(doc)
			if len(tokens) == 0 {
				continue
			}

			fi.docWeights[docID] = field.Weight
			fi.docLens[docID] = len(tokens)
			fieldStats[field.Name].totalLen += len(tokens)
			fieldStats[field.Name].docs++

			tfMap := make(map[string]int, len(tokens))
			for _, tok := range tokens {
				tfMap[tok]++
			}
			tfs := make([]termFreq, 0, len(tfMap))
			for term, count := range tfMap {
				tfs = append(tfs, termFreq{Term: term, Count: count})
				fi.posting[term] = append(fi.posting[term], docID)
				fieldStats[field.Name].df[term]++
			}
			slices.SortFunc(tfs, func(a, b termFreq) int { return cmp.Compare(a.Term, b.Term) })
			fi.termFreqs[docID] = tfs
		}
	}

	for name, fi := range idx.fields {
		st := fieldStats[name]
		if st.docs > 0 {
			fi.avgDocLen = float64(st.totalLen) / float64(st.docs)
		} else {
			fi.avgDocLen = 1
		}
		nDocs := max(1, st.docs)
		for term, d := range st.df {
			fi.idf[term] = math.Log((float64(nDocs)-float64(d)+0.5)/(float64(d)+0.5) + 1)
		}
	}

	return idx
}

func (fi *bm25FieldIndex) scoreDoc(docID int, queryTerms []string, k1, b float64) float64 {
	if fi.docWeights[docID] == 0 {
		return 0
	}
	tfs := fi.termFreqs[docID]
	dl := float64(fi.docLens[docID])
	var score float64
	for _, term := range queryTerms {
		idf, ok := fi.idf[term]
		if !ok {
			continue
		}
		freq := 0.0
		if j, found := slices.BinarySearchFunc(tfs, term, func(tf termFreq, target string) int {
			return cmp.Compare(tf.Term, target)
		}); found {
			freq = float64(tfs[j].Count)
		}
		if freq == 0 {
			continue
		}
		score += idf * (freq * (k1 + 1)) / (freq + k1*(1-b+b*dl/fi.avgDocLen))
	}
	return score
}

func (idx *bm25Index) maxQueryIDF(queryTerms []string) float64 {
	maxIDF := 1.0
	for _, term := range queryTerms {
		for _, fieldIdx := range idx.fields {
			if idf, ok := fieldIdx.idf[term]; ok && idf > maxIDF {
				maxIDF = idf
			}
		}
	}
	return maxIDF
}

func (idx *bm25Index) Search(query string) []bm25SearchResult {
	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	seenPtr := idx.seenPool.Get().(*[]uint8)
	seen := *seenPtr
	var touched []int

	for _, term := range queryTerms {
		for _, fieldIdx := range idx.fields {
			for _, docID := range fieldIdx.posting[term] {
				if seen[docID] == 0 {
					touched = append(touched, docID)
				}
				seen[docID] |= 1
			}
		}
		for _, docID := range idx.prefixPosting[term] {
			if seen[docID] == 0 {
				touched = append(touched, docID)
			}
			seen[docID] |= 2
		}
	}
	if len(touched) == 0 {
		idx.seenPool.Put(seenPtr)
		return nil
	}

	prefixBonus := 0.5 * idx.maxQueryIDF(queryTerms)
	results := make([]bm25SearchResult, 0, len(touched))
	for _, docID := range touched {
		s := seen[docID]
		isPrefix := (s & 2) != 0
		score := 0.0
		for _, fieldIdx := range idx.fields {
			score += fieldIdx.docWeights[docID] * fieldIdx.scoreDoc(docID, queryTerms, idx.k1, idx.b)
		}
		if isPrefix {
			score += idx.primaryWeights[docID] * prefixBonus
		}
		if score > 0 {
			results = append(results, bm25SearchResult{
				Doc:         docID,
				Score:       score,
				PrefixMatch: isPrefix,
			})
		}
		seen[docID] = 0 // reset
	}
	idx.seenPool.Put(seenPtr)

	slices.SortFunc(results, func(a, b bm25SearchResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(a.Doc, b.Doc)
	})
	return results
}

func (idx *bm25Index) snapshot() bm25Snapshot {
	fields := make(map[string]bm25FieldSnapshot, len(idx.fields))
	for name, fieldIdx := range idx.fields {
		fields[name] = bm25FieldSnapshot{
			IDF:        fieldIdx.idf,
			TermFreqs:  fieldIdx.termFreqs,
			DocLens:    fieldIdx.docLens,
			DocWeights: fieldIdx.docWeights,
			AvgDocLen:  fieldIdx.avgDocLen,
			Posting:    fieldIdx.posting,
		}
	}
	return bm25Snapshot{
		Fields:         fields,
		PrefixPosting:  idx.prefixPosting,
		PrimaryWeights: idx.primaryWeights,
		K1:             idx.k1,
		B:              idx.b,
	}
}

func bm25FromSnapshot(s bm25Snapshot) *bm25Index {
	fields := make(map[string]*bm25FieldIndex, len(s.Fields))
	for name, snap := range s.Fields {
		fields[name] = &bm25FieldIndex{
			idf:        snap.IDF,
			termFreqs:  snap.TermFreqs,
			docLens:    snap.DocLens,
			docWeights: snap.DocWeights,
			avgDocLen:  snap.AvgDocLen,
			posting:    snap.Posting,
		}
	}
	idx := &bm25Index{
		fields:         fields,
		prefixPosting:  s.PrefixPosting,
		primaryWeights: s.PrimaryWeights,
		k1:             s.K1,
		b:              s.B,
	}
	n := len(s.PrimaryWeights)
	idx.seenPool.New = func() any {
		buf := make([]uint8, n)
		return &buf
	}
	return idx
}

