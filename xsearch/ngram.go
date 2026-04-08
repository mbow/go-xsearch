package xsearch

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

type ngramFieldIndex struct {
	posting       map[string][]int
	docGramCounts []int
	docWeights    []float64
	hitsPool      sync.Pool
}

type prefixEntry struct {
	Value string  `cbor:"value"`
	Doc   int     `cbor:"doc"`
	Score float64 `cbor:"score"`
}

type ngramIndex struct {
	fields        map[string]*ngramFieldIndex
	sortedPrimary []prefixEntry
}

type ngramFieldSnapshot struct {
	Posting       map[string][]int `cbor:"posting"`
	DocGramCounts []int            `cbor:"doc_gram_counts"`
	DocWeights    []float64        `cbor:"doc_weights"`
}

type ngramSnapshot struct {
	Fields        map[string]ngramFieldSnapshot `cbor:"fields"`
	SortedPrimary []prefixEntry                 `cbor:"sorted_primary"`
}

type ngramSearchResult struct {
	Doc   int
	Score float64
}

func newNgramIndex(items []preparedItem) *ngramIndex {
	n := len(items)
	idx := &ngramIndex{
		fields: make(map[string]*ngramFieldIndex),
	}

	for docID, item := range items {
		primary := item.Fields[item.primaryField]
		for _, value := range primary.Values {
			idx.sortedPrimary = append(idx.sortedPrimary, prefixEntry{
				Value: normalizeQuery(value),
				Doc:   docID,
				Score: primary.Weight,
			})
		}

		for _, field := range item.Fields {
			fi, ok := idx.fields[field.Name]
			if !ok {
				fi = &ngramFieldIndex{
					posting:       make(map[string][]int),
					docGramCounts: make([]int, n),
					docWeights:    make([]float64, n),
				}
				idx.fields[field.Name] = fi
			}
			unique := make(map[string]struct{})
			for _, value := range field.Values {
				grams := extractNormalizedTrigrams(nil, normalizeQuery(value))
				for _, gram := range grams {
					unique[gram] = struct{}{}
				}
			}
			if len(unique) == 0 {
				continue
			}
			fi.docWeights[docID] = field.Weight
			fi.docGramCounts[docID] = len(unique)
			for gram := range unique {
				fi.posting[gram] = append(fi.posting[gram], docID)
			}
		}
	}

	for _, fi := range idx.fields {
		fi.hitsPool = sync.Pool{New: func() any {
			s := make([]uint8, n)
			return &s
		}}
	}

	slices.SortFunc(idx.sortedPrimary, func(a, b prefixEntry) int {
		if a.Value != b.Value {
			return cmp.Compare(a.Value, b.Value)
		}
		return cmp.Compare(a.Doc, b.Doc)
	})

	return idx
}

// Search expects a pre-normalized (lowercased, trimmed) query.
func (idx *ngramIndex) Search(query string) []ngramSearchResult {
	if query == "" {
		return nil
	}
	if len(query) < 3 {
		return idx.prefixSearch(query)
	}

	var trigramBuf [16]string
	queryGrams := extractNormalizedTrigrams(trigramBuf[:0], query)
	slices.Sort(queryGrams)
	queryGrams = slices.Compact(queryGrams)
	return idx.SearchWithGrams(queryGrams)
}

func (idx *ngramIndex) SearchWithGrams(queryGrams []string) []ngramSearchResult {
	if len(queryGrams) == 0 {
		return nil
	}
	accum := make(map[int]float64)
	for _, fieldIdx := range idx.fields {
		fieldIdx.searchWithGramsInto(queryGrams, accum)
	}
	results := make([]ngramSearchResult, 0, len(accum))
	for docID, score := range accum {
		if score <= 0 {
			continue
		}
		results = append(results, ngramSearchResult{Doc: docID, Score: score})
	}
	slices.SortFunc(results, func(a, b ngramSearchResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(a.Doc, b.Doc)
	})
	return results
}

type gramEntry struct {
	gram string
	size int
}

func (fi *ngramFieldIndex) searchWithGramsInto(queryGrams []string, accum map[int]float64) {
	if len(queryGrams) == 0 {
		return
	}

	var sortBuf [16]gramEntry
	sorted := sortBuf[:0]
	for _, gram := range queryGrams {
		sorted = append(sorted, gramEntry{gram: gram, size: len(fi.posting[gram])})
	}
	slices.SortFunc(sorted, func(a, b gramEntry) int {
		return cmp.Compare(a.size, b.size)
	})

	hitsPtr := fi.hitsPool.Get().(*[]uint8)
	hits := *hitsPtr

	const (
		maxPostingExpand = 200
		maxCandidates    = 500
	)

	var touchBuf [256]int
	touched := touchBuf[:0]
	for _, docID := range fi.posting[sorted[0].gram] {
		if hits[docID] < 255 {
			hits[docID]++
		}
		touched = append(touched, docID)
	}
	for _, entry := range sorted[1:] {
		posting := fi.posting[entry.gram]
		expand := len(posting) <= maxPostingExpand && len(touched) < maxCandidates
		for _, docID := range posting {
			if hits[docID] > 0 {
				if hits[docID] < 255 {
					hits[docID]++
				}
			} else if expand {
				hits[docID]++
				touched = append(touched, docID)
			}
		}
	}

	minHits := uint8(min(255, max(1, len(queryGrams)/3)))

	for _, docID := range touched {
		h := hits[docID]
		if h < minHits {
			continue
		}
		intersection := int(h)
		unionSize := len(queryGrams) + fi.docGramCounts[docID] - intersection
		if unionSize <= 0 || fi.docWeights[docID] <= 0 {
			continue
		}
		accum[docID] += (float64(intersection) / float64(unionSize)) * fi.docWeights[docID]
	}

	for _, docID := range touched {
		hits[docID] = 0
	}
	fi.hitsPool.Put(hitsPtr)
}

func (idx *ngramIndex) prefixSearch(query string) []ngramSearchResult {
	lo, _ := slices.BinarySearchFunc(idx.sortedPrimary, query, func(entry prefixEntry, target string) int {
		return cmp.Compare(entry.Value, target)
	})

	seen := make(map[int]float64)
	for i := lo; i < len(idx.sortedPrimary); i++ {
		if !strings.HasPrefix(idx.sortedPrimary[i].Value, query) {
			break
		}
		if _, ok := seen[idx.sortedPrimary[i].Doc]; !ok {
			seen[idx.sortedPrimary[i].Doc] = idx.sortedPrimary[i].Score
		}
	}

	results := make([]ngramSearchResult, 0, len(seen))
	for docID, score := range seen {
		results = append(results, ngramSearchResult{Doc: docID, Score: score})
	}
	slices.SortFunc(results, func(a, b ngramSearchResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(a.Doc, b.Doc)
	})
	return results
}

func (idx *ngramIndex) snapshot() ngramSnapshot {
	fields := make(map[string]ngramFieldSnapshot, len(idx.fields))
	for name, fi := range idx.fields {
		fields[name] = ngramFieldSnapshot{
			Posting:       fi.posting,
			DocGramCounts: fi.docGramCounts,
			DocWeights:    fi.docWeights,
		}
	}
	return ngramSnapshot{
		Fields:        fields,
		SortedPrimary: idx.sortedPrimary,
	}
}

func ngramFromSnapshot(s ngramSnapshot, n int) *ngramIndex {
	fields := make(map[string]*ngramFieldIndex, len(s.Fields))
	for name, snap := range s.Fields {
		fi := &ngramFieldIndex{
			posting:       snap.Posting,
			docGramCounts: snap.DocGramCounts,
			docWeights:    snap.DocWeights,
		}
		fi.hitsPool = sync.Pool{New: func() any {
			buf := make([]uint8, n)
			return &buf
		}}
		fields[name] = fi
	}
	return &ngramIndex{
		fields:        fields,
		sortedPrimary: s.SortedPrimary,
	}
}

type fallbackIndex struct {
	groupDocs  map[string][]int
	groupGrams map[string][]string
	gramGroups map[string][]string
}

func newFallbackIndex(items []preparedItem, fieldName string) *fallbackIndex {
	if fieldName == "" {
		return nil
	}
	idx := &fallbackIndex{
		groupDocs:  make(map[string][]int),
		groupGrams: make(map[string][]string),
		gramGroups: make(map[string][]string),
	}

	for docID, item := range items {
		for _, field := range item.Fields {
			if field.Name != fieldName {
				continue
			}
			seen := make(map[string]struct{})
			for _, value := range field.Values {
				key := normalizeQuery(value)
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				idx.groupDocs[key] = append(idx.groupDocs[key], docID)
			}
		}
	}

	for key := range idx.groupDocs {
		grams := extractNormalizedTrigrams(nil, key)
		slices.Sort(grams)
		grams = slices.Compact(grams)
		idx.groupGrams[key] = grams
		for _, gram := range grams {
			idx.gramGroups[gram] = append(idx.gramGroups[gram], key)
		}
	}

	return idx
}

func (idx *fallbackIndex) exact(query string) ([]int, bool) {
	if idx == nil {
		return nil, false
	}
	docs, ok := idx.groupDocs[normalizeQuery(query)]
	return docs, ok
}

func (idx *fallbackIndex) best(query string, queryGrams []string) ([]int, bool) {
	if idx == nil {
		return nil, false
	}
	query = normalizeQuery(query)
	if query == "" {
		return nil, false
	}
	if len(queryGrams) == 0 {
		best := ""
		for key := range idx.groupDocs {
			if strings.HasPrefix(key, query) && (best == "" || key < best) {
				best = key
			}
		}
		if best == "" {
			return nil, false
		}
		return idx.groupDocs[best], true
	}

	bestName := ""
	bestScore := 0.0
	candidates := make(map[string]int, len(queryGrams))
	for _, gram := range queryGrams {
		for _, key := range idx.gramGroups[gram] {
			candidates[key]++
		}
	}
	for key, intersection := range candidates {
		grams := idx.groupGrams[key]
		if intersection == 0 {
			continue
		}
		unionSize := len(queryGrams) + len(grams) - intersection
		score := float64(intersection) / float64(unionSize)
		if score > bestScore || (score == bestScore && (bestName == "" || key < bestName)) {
			bestName = key
			bestScore = score
		}
	}
	if bestName == "" {
		return nil, false
	}
	return idx.groupDocs[bestName], true
}

