package xsearch

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Field represents a named, weighted searchable field with one or more values.
type Field struct {
	Name   string
	Values []string
	Weight float64
}

// Searchable is implemented by any type that can be indexed and searched.
type Searchable interface {
	SearchID() string
	SearchFields() []Field
}

// Scorer provides an external score for a searchable item lock-free.
type Scorer interface {
	Score(docIndex int) float64
}

// SearchOption configures options for a single search execution.
type SearchOption func(*searchConfig)

type searchConfig struct {
	scorer Scorer
}

// WithScoring applies a request-scoped scorer.
func WithScoring(s Scorer) SearchOption {
	return func(c *searchConfig) {
		c.scorer = s
	}
}

// Item is the stored representation of an indexed document.
// It is returned by Engine.Get for rendering, lookup, or consumer-side hydration.
type Item struct {
	ID     string
	Fields []Field
}

// Engine is the core search engine.
type Engine struct {
	cfg      engineConfig
	items    []preparedItem
	idToDoc  map[string]int
	ordered  []string
	bloom    *Bloom
	bm25     *bm25Index
	ngram    *ngramIndex
	fallback *fallbackIndex
}

type scoredCandidate struct {
	doc       int
	relevance float64
	matchType MatchType
	score     float64
}

func candidateBetter(a, b scoredCandidate) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.doc < b.doc
}

func worstCandidateIndex(candidates []scoredCandidate) int {
	worst := 0
	for i := 1; i < len(candidates); i++ {
		if candidateBetter(candidates[worst], candidates[i]) {
			worst = i
		}
	}
	return worst
}

// New creates a search engine from a slice of Searchable items.
func New[T Searchable](items []T, opts ...Option) (*Engine, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt.applyTo(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	prepared := make([]preparedItem, len(items))
	seenIDs := make(map[string]struct{}, len(items))
	for i, item := range items {
		id := item.SearchID()
		if id == "" {
			return nil, fmt.Errorf("xsearch: item at index %d has empty ID", i)
		}
		if _, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("xsearch: duplicate ID %q", id)
		}
		seenIDs[id] = struct{}{}

		fields, primaryField, err := prepareFields(id, item.SearchFields())
		if err != nil {
			return nil, err
		}
		prepared[i] = preparedItem{
			ID:           id,
			Fields:       fields,
			primaryField: primaryField,
		}
	}

	return newEngineFromPrepared(prepared, cfg), nil
}

func newEngineFromPrepared(items []preparedItem, cfg engineConfig) *Engine {
	e := &Engine{
		cfg:     cfg,
		items:   items,
		idToDoc: make(map[string]int, len(items)),
		ordered: make([]string, len(items)),
	}
	for i, item := range items {
		e.idToDoc[item.ID] = i
		e.ordered[i] = item.ID
	}
	if cfg.bloomBitsPerItem > 0 {
		e.bloom = NewBloom(len(items), cfg.bloomBitsPerItem)
		for _, item := range items {
			for _, field := range item.Fields {
				for _, value := range field.Values {
					for _, gram := range extractNormalizedTrigrams(nil, normalizeQuery(value)) {
						e.bloom.Add(gram)
					}
				}
			}
		}
	}
	e.bm25 = newBM25Index(items, cfg)
	e.ngram = newNgramIndex(items)
	e.fallback = newFallbackIndex(items, cfg.fallbackField)
	return e
}

func prepareFields(id string, fields []Field) ([]internalField, int, error) {
	cloned := cloneFields(fields)
	if len(cloned) == 0 {
		return nil, 0, fmt.Errorf("xsearch: item %q has no fields", id)
	}
	out := make([]internalField, 0, len(cloned))
	fieldNames := make(map[string]struct{}, len(cloned))
	primaryField := -1
	primaryWeight := -1.0
	for _, field := range cloned {
		if field.Name == "" {
			return nil, 0, fmt.Errorf("xsearch: item %q has field with empty name", id)
		}
		if _, exists := fieldNames[field.Name]; exists {
			return nil, 0, fmt.Errorf("xsearch: item %q has duplicate field name %q", id, field.Name)
		}
		fieldNames[field.Name] = struct{}{}
		if field.Weight <= 0 {
			return nil, 0, fmt.Errorf("xsearch: item %q field %q has non-positive weight %f", id, field.Name, field.Weight)
		}
		if len(field.Values) == 0 {
			continue
		}
		lowerValues := make([]string, len(field.Values))
		for i, v := range field.Values {
			lowerValues[i] = strings.ToLower(v)
		}

		out = append(out, internalField{
			Name:        field.Name,
			Values:      slices.Clone(field.Values),
			LowerValues: lowerValues,
			Weight:      field.Weight,
		})
		if field.Weight > primaryWeight {
			primaryField = len(out) - 1
			primaryWeight = field.Weight
		}
	}
	if len(out) == 0 || primaryField < 0 {
		return nil, 0, fmt.Errorf("xsearch: item %q has no indexable field values", id)
	}
	return out, primaryField, nil
}

// Search returns results matching the query, ordered by combined score.
func (e *Engine) Search(query string, opts ...SearchOption) []Result {
	var sCfg searchConfig
	for _, opt := range opts {
		opt(&sCfg)
	}

	query = normalizeQuery(query)
	if query == "" {
		return nil
	}

	if docs, ok := e.fallback.exact(query); ok {
		return e.resultsForCandidates(query, fallbackCandidates(docs, nil), sCfg)
	}

	var trigramBuf [16]string
	queryGrams := extractNormalizedTrigrams(trigramBuf[:0], query)
	if len(queryGrams) > 0 {
		slices.Sort(queryGrams)
		queryGrams = slices.Compact(queryGrams)
	}

	direct := make(map[int]scoredCandidate)

	if len(queryGrams) == 0 {
		if bm25Results := e.bm25.Search(query); len(bm25Results) > 0 {
			for _, result := range bm25Results {
				direct[result.Doc] = scoredCandidate{
					doc:       result.Doc,
					relevance: result.Score,
					matchType: MatchDirect,
				}
			}
		} else {
			for _, result := range e.ngram.Search(query) {
				direct[result.Doc] = scoredCandidate{
					doc:       result.Doc,
					relevance: result.Score,
					matchType: MatchDirect,
				}
			}
		}
	} else {
		if e.bloom != nil {
			anyPass := false
			for _, gram := range queryGrams {
				if e.bloom.MayContain(gram) {
					anyPass = true
					break
				}
			}
			if !anyPass {
				if docs, ok := e.fallback.best(query, queryGrams); ok {
					return e.resultsForCandidates(query, fallbackCandidates(docs, nil), sCfg)
				}
				return nil
			}
		}

		if bm25Results := e.bm25.Search(query); len(bm25Results) > 0 {
			for _, result := range bm25Results {
				direct[result.Doc] = scoredCandidate{
					doc:       result.Doc,
					relevance: result.Score,
					matchType: MatchDirect,
				}
			}
		} else {
			for _, result := range e.ngram.SearchWithGrams(queryGrams) {
				direct[result.Doc] = scoredCandidate{
					doc:       result.Doc,
					relevance: result.Score,
					matchType: MatchDirect,
				}
			}
		}
	}

	if len(direct) < defaultMinDirectResults {
		if docs, ok := e.fallback.best(query, queryGrams); ok {
			for docID, cand := range fallbackCandidates(docs, direct) {
				direct[docID] = cand
			}
		}
	}

	return e.resultsForCandidates(query, direct, sCfg)
}

func fallbackCandidates(docs []int, existing map[int]scoredCandidate) map[int]scoredCandidate {
	out := make(map[int]scoredCandidate, len(docs))
	for _, docID := range docs {
		if existing != nil {
			if _, ok := existing[docID]; ok {
				continue
			}
		}
		out[docID] = scoredCandidate{
			doc:       docID,
			relevance: fallbackRelevance,
			matchType: MatchFallback,
		}
	}
	return out
}

func (e *Engine) resultsForCandidates(query string, candidates map[int]scoredCandidate, sCfg searchConfig) []Result {
	if len(candidates) == 0 {
		return nil
	}

	var rawScores map[int]float64
	maxRaw := 0.0
	if sCfg.scorer != nil {
		rawScores = make(map[int]float64, len(candidates))
		for docID := range candidates {
			raw := sanitizeScorerValue(sCfg.scorer.Score(docID))
			rawScores[docID] = raw
			if raw > maxRaw {
				maxRaw = raw
			}
		}
	}

	limit := e.cfg.limit
	if limit <= 0 {
		limit = 10
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}
	scored := make([]scoredCandidate, 0, limit)

	for docID, cand := range candidates {
		if rawScores == nil {
			cand.score = cand.relevance
		} else {
			norm := 0.0
			if maxRaw > 0 {
				norm = rawScores[docID] / maxRaw
			}
			cand.score = (1-e.cfg.alpha)*cand.relevance + e.cfg.alpha*norm
		}

		if len(scored) < limit {
			scored = append(scored, cand)
			continue
		}

		worst := worstCandidateIndex(scored)
		if candidateBetter(cand, scored[worst]) {
			scored[worst] = cand
		}
	}

	slices.SortFunc(scored, func(a, b scoredCandidate) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(a.doc, b.doc)
	})

	results := make([]Result, len(scored))
	for i, cand := range scored {
		results[i] = Result{
			ID:         e.items[cand.doc].ID,
			Score:      cand.score,
			MatchType:  cand.matchType,
			Highlights: computeHighlights(e.items[cand.doc], query),
		}
	}
	return results
}

func computeHighlights(item preparedItem, query string) map[string][]Highlight {
	var wordBuf [8]string
	queryWords := appendQueryWords(wordBuf[:0], query)

	var out map[string][]Highlight
	for _, field := range item.Fields {
		var highlights []Highlight
		for valueIndex := range field.Values {
			lowerValue := field.Values[valueIndex]
			if valueIndex < len(field.LowerValues) {
				lowerValue = field.LowerValues[valueIndex]
			} else {
				lowerValue = toLowerFast(lowerValue)
			}

			var localBuf [8]Highlight
			local := localBuf[:0]
			for _, word := range queryWords {
				pos := strings.Index(lowerValue, word)
				if pos >= 0 {
					local = append(local, Highlight{
						Start:      pos,
						End:        pos + len(word),
						ValueIndex: valueIndex,
					})
				}
			}
			if len(local) == 0 {
				if pos := strings.Index(lowerValue, query); pos >= 0 {
					local = append(local, Highlight{
						Start:      pos,
						End:        pos + len(query),
						ValueIndex: valueIndex,
					})
				}
			}
			if len(local) > 0 {
				highlights = append(highlights, local...)
			}
		}
		if len(highlights) > 0 {
			if out == nil {
				out = make(map[string][]Highlight)
			}
			out[field.Name] = mergeHighlights(highlights)
		}
	}
	if out == nil {
		return nil
	}
	return out
}

// Get returns the indexed item by ID, including original field values.
func (e *Engine) Get(id string) (Item, bool) {
	docID, ok := e.idToDoc[id]
	if !ok {
		return Item{}, false
	}
	return cloneItem(e.items[docID]), true
}

// IDs returns the ordered list of indexed IDs.
func (e *Engine) IDs() []string {
	return slices.Clone(e.ordered)
}
