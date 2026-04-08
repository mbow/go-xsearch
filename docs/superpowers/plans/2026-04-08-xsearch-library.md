# xsearch Library Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the core search engine into a domain-agnostic Go library (`xsearch/` package) with clean interfaces, then adapt the sample app to consume it.

**Architecture:** Bottom-up build of the `xsearch/` package: types/interfaces first, then internal components (bloom, n-gram, BM25), then the Engine that wires them together, then snapshots. After the library is complete and tested, adapt the sample app (catalog, ranking, server, main, generate) to consume it, update benchmarks, and delete the old packages.

**Tech Stack:** Go 1.26, CBOR (github.com/fxamacker/cbor/v2), TDD throughout.

**Spec:** `docs/superpowers/specs/2026-04-08-xsearch-library-design.md`

---

## File Structure

### New files (the library)

| File | Responsibility |
|------|---------------|
| `xsearch/xsearch.go` | `Engine`, `Searchable`, `Scorer`, `Field`, `Item`, `Option`, `New()`, `Search()`, `Get()` |
| `xsearch/result.go` | `Result`, `MatchType`, `Highlight` |
| `xsearch/config.go` | `engineConfig`, option functions, validation |
| `xsearch/bloom.go` | `Bloom`, `NewBloom()`, `Add()`, `MayContain()` |
| `xsearch/ngram.go` | unexported n-gram index adapted for `[]Field` with per-field weights |
| `xsearch/bm25.go` | unexported BM25 index adapted for per-field scoring |
| `xsearch/snapshot.go` | CBOR serialization: `Snapshot()`, `NewFromSnapshot()`, version header |
| `xsearch/xsearch_test.go` | Engine integration tests, validation tests |
| `xsearch/bloom_test.go` | Bloom filter unit tests |
| `xsearch/ngram_test.go` | N-gram unit tests |
| `xsearch/bm25_test.go` | BM25 unit tests |
| `xsearch/snapshot_test.go` | Snapshot round-trip tests |
| `xsearch/bench_test.go` | Library-level benchmarks |

### Modified files (sample app adaptation)

| File | Changes |
|------|---------|
| `catalog/catalog.go` | Add `SearchID()`, `SearchFields()` methods to `Product` |
| `catalog/embed.go` | Simplify: single embedded CBOR blob, expose raw bytes for `xsearch.NewFromSnapshot` |
| `ranking/ranking.go` | String IDs: `map[string][]time.Time`, `Score(id string) float64` |
| `internal/server/server.go` | Use `xsearch.Engine`, build highlights from `xsearch.Result`, string ID in `/select` |
| `main.go` | Wire `xsearch.NewFromSnapshot()` with options |
| `cmd/generate/main.go` | Use `xsearch.New()` + `Snapshot()` |
| `benchmarks/suite_test.go` | Update imports from old packages to `xsearch`, adapt IDs |

### Deleted after migration

| Package | Reason |
|---------|--------|
| `bloom/` | Moved to `xsearch/bloom.go` |
| `index/` | Moved to `xsearch/ngram.go` |
| `bm25/` | Moved to `xsearch/bm25.go` |
| `engine/` | Replaced by `xsearch/xsearch.go` |

---

### Task 1: Core Types and Interfaces

**Files:**
- Create: `xsearch/result.go`
- Create: `xsearch/config.go`
- Create: `xsearch/xsearch.go` (interfaces + stubs only)
- Create: `xsearch/xsearch_test.go`

- [ ] **Step 1: Write validation tests**

```go
// xsearch/xsearch_test.go
package xsearch

import (
	"testing"
)

type testItem struct {
	id     string
	fields []Field
}

func (t testItem) SearchID() string      { return t.id }
func (t testItem) SearchFields() []Field { return t.fields }

func TestNew_RejectsDuplicateIDs(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 1.0}}},
		testItem{id: "a", fields: []Field{{Name: "name", Values: []string{"also alpha"}, Weight: 1.0}}},
	}
	_, err := New(items)
	if err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
}

func TestNew_RejectsEmptyID(t *testing.T) {
	items := []Searchable{
		testItem{id: "", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 1.0}}},
	}
	_, err := New(items)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestNew_RejectsNonPositiveWeight(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 0.0}}},
	}
	_, err := New(items)
	if err == nil {
		t.Fatal("expected error for non-positive weight")
	}
}

func TestNew_RejectsEmptyFieldName(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{{Name: "", Values: []string{"alpha"}, Weight: 1.0}}},
	}
	_, err := New(items)
	if err == nil {
		t.Fatal("expected error for empty field name")
	}
}

func TestNew_RejectsDuplicateFieldNames(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{
			{Name: "name", Values: []string{"alpha"}, Weight: 1.0},
			{Name: "name", Values: []string{"beta"}, Weight: 0.5},
		}},
	}
	_, err := New(items)
	if err == nil {
		t.Fatal("expected error for duplicate field names")
	}
}

func TestWithLimit_Validation(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 1.0}}},
	}

	_, err := New(items, WithLimit(1))
	if err == nil {
		t.Fatal("expected error for limit < 2")
	}

	_, err = New(items, WithLimit(101))
	if err == nil {
		t.Fatal("expected error for limit > 100")
	}

	_, err = New(items, WithLimit(10))
	if err != nil {
		t.Fatalf("unexpected error for valid limit: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestNew_|TestWithLimit'`
Expected: Compilation errors — types don't exist yet.

- [ ] **Step 3: Write the types and interfaces**

```go
// xsearch/result.go
package xsearch

// MatchType indicates how a result was found.
type MatchType int

const (
	MatchDirect   MatchType = iota // Found via direct n-gram or BM25 match
	MatchFallback                  // Found via fallback group (see WithFallbackField)
)

// Highlight marks a matched byte range within a field value.
type Highlight struct {
	Start      int // byte offset (inclusive)
	End        int // byte offset (exclusive)
	ValueIndex int // index into the matched Field.Values slice
}

// Result is a single search result with metadata.
type Result struct {
	ID         string
	Score      float64
	MatchType  MatchType
	Highlights map[string][]Highlight // field name -> highlight spans
}
```

```go
// xsearch/config.go
package xsearch

import "fmt"

// engineConfig holds all configuration for an Engine.
type engineConfig struct {
	scorer        Scorer
	bloomBPI      int     // bits per item; 0 = disabled
	bm25K1        float64
	bm25B         float64
	alpha         float64
	limit         int
	fallbackField string
}

func defaultConfig() engineConfig {
	return engineConfig{
		bm25K1: 1.2,
		bm25B:  0.75,
		alpha:  0.6,
		limit:  10,
	}
}

// Option configures the engine.
type Option func(*engineConfig)

// WithScorer sets an external scorer to blend with relevance.
func WithScorer(s Scorer) Option {
	return func(c *engineConfig) { c.scorer = s }
}

// WithBloom enables bloom filter pre-rejection.
// bitsPerItem controls false-positive rate. Set to 0 to disable.
func WithBloom(bitsPerItem int) Option {
	return func(c *engineConfig) { c.bloomBPI = bitsPerItem }
}

// WithBM25 configures BM25 parameters.
func WithBM25(k1, b float64) Option {
	return func(c *engineConfig) { c.bm25K1 = k1; c.bm25B = b }
}

// WithAlpha sets the blending weight between relevance and external score.
// 0.0 = relevance only, 1.0 = external score only.
func WithAlpha(alpha float64) Option {
	return func(c *engineConfig) { c.alpha = alpha }
}

// WithLimit sets the maximum number of results. Must be in [2, 100].
func WithLimit(n int) Option {
	return func(c *engineConfig) { c.limit = n }
}

// WithFallbackField sets the field name used for group-based fallback.
func WithFallbackField(fieldName string) Option {
	return func(c *engineConfig) { c.fallbackField = fieldName }
}

// validate checks the config after all options are applied.
func (c *engineConfig) validate() error {
	if c.limit < 2 || c.limit > 100 {
		return fmt.Errorf("xsearch: limit must be in [2, 100], got %d", c.limit)
	}
	return nil
}
```

```go
// xsearch/xsearch.go
package xsearch

import "fmt"

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

// Scorer provides an external score for a searchable item.
type Scorer interface {
	Score(id string) float64
}

// Item is the stored representation of an indexed document.
type Item struct {
	ID     string
	Fields []Field
}

// Engine is the core search engine.
type Engine struct {
	cfg   engineConfig
	items []Item // stored items for Get()
	ids   map[string]int // id -> index
}

// New creates a search engine from a slice of Searchable items.
func New(items []Searchable, opts ...Option) (*Engine, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	e := &Engine{
		cfg:   cfg,
		items: make([]Item, len(items)),
		ids:   make(map[string]int, len(items)),
	}

	for i, item := range items {
		id := item.SearchID()
		if id == "" {
			return nil, fmt.Errorf("xsearch: item at index %d has empty ID", i)
		}
		if _, exists := e.ids[id]; exists {
			return nil, fmt.Errorf("xsearch: duplicate ID %q", id)
		}

		fields := item.SearchFields()
		fieldNames := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			if f.Name == "" {
				return nil, fmt.Errorf("xsearch: item %q has field with empty name", id)
			}
			if _, exists := fieldNames[f.Name]; exists {
				return nil, fmt.Errorf("xsearch: item %q has duplicate field name %q", id, f.Name)
			}
			if f.Weight <= 0 {
				return nil, fmt.Errorf("xsearch: item %q field %q has non-positive weight %f", id, f.Name, f.Weight)
			}
			fieldNames[f.Name] = struct{}{}
		}

		e.items[i] = Item{ID: id, Fields: fields}
		e.ids[id] = i
	}

	return e, nil
}

// Search returns results matching the query, ordered by combined score.
func (e *Engine) Search(query string) []Result {
	// TODO: implement in Task 5
	return nil
}

// Get returns the indexed item by ID.
// Returned data is a copy and safe for the caller to retain.
func (e *Engine) Get(id string) (Item, bool) {
	idx, ok := e.ids[id]
	if !ok {
		return Item{}, false
	}
	src := e.items[idx]
	fields := make([]Field, len(src.Fields))
	for i, f := range src.Fields {
		vals := make([]string, len(f.Values))
		copy(vals, f.Values)
		fields[i] = Field{Name: f.Name, Values: vals, Weight: f.Weight}
	}
	return Item{ID: src.ID, Fields: fields}, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestNew_|TestWithLimit'`
Expected: All 6 tests PASS.

- [ ] **Step 5: Write and run Get tests**

Add to `xsearch/xsearch_test.go`:

```go
func TestGet_ReturnsItem(t *testing.T) {
	items := []Searchable{
		testItem{id: "beer-1", fields: []Field{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
	}
	eng, err := New(items)
	if err != nil {
		t.Fatal(err)
	}

	item, ok := eng.Get("beer-1")
	if !ok {
		t.Fatal("expected to find item")
	}
	if item.ID != "beer-1" {
		t.Fatalf("expected ID beer-1, got %s", item.ID)
	}
	if len(item.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(item.Fields))
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{
			{Name: "name", Values: []string{"Alpha"}, Weight: 1.0},
		}},
	}
	eng, err := New(items)
	if err != nil {
		t.Fatal(err)
	}

	item1, _ := eng.Get("a")
	item1.Fields[0].Values[0] = "MUTATED"

	item2, _ := eng.Get("a")
	if item2.Fields[0].Values[0] != "Alpha" {
		t.Fatal("Get returned shared slice, not a copy")
	}
}

func TestGet_NotFound(t *testing.T) {
	items := []Searchable{
		testItem{id: "a", fields: []Field{{Name: "name", Values: []string{"Alpha"}, Weight: 1.0}}},
	}
	eng, err := New(items)
	if err != nil {
		t.Fatal(err)
	}

	_, ok := eng.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}
```

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestGet'`
Expected: All 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add xsearch/
git commit -m "feat(xsearch): add core types, interfaces, options, and validation"
```

---

### Task 2: Bloom Filter

**Files:**
- Create: `xsearch/bloom.go`
- Create: `xsearch/bloom_test.go`

- [ ] **Step 1: Write bloom filter tests**

```go
// xsearch/bloom_test.go
package xsearch

import "testing"

func TestBloom_NoFalseNegatives(t *testing.T) {
	bf := NewBloom(1000, 10)
	keys := []string{"budweiser", "corona", "heineken", "stella", "guinness"}
	for _, k := range keys {
		bf.Add(k)
	}
	for _, k := range keys {
		if !bf.MayContain(k) {
			t.Fatalf("false negative for %q", k)
		}
	}
}

func TestBloom_RejectsAbsentKeys(t *testing.T) {
	bf := NewBloom(100, 100) // high bits-per-item = very low FP rate
	bf.Add("present")

	// With 100 bits per item and only 1 item, FP rate is near zero.
	absent := []string{"absent", "missing", "nowhere", "gone", "nope"}
	for _, k := range absent {
		if bf.MayContain(k) {
			t.Fatalf("unexpected positive for %q (possible but very unlikely)", k)
		}
	}
}

func TestBloom_EmptyFilter(t *testing.T) {
	bf := NewBloom(100, 10)
	if bf.MayContain("anything") {
		t.Fatal("empty filter should not contain anything")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestBloom'`
Expected: Compilation error — `NewBloom` not defined.

- [ ] **Step 3: Implement Bloom filter**

Port from `bloom/bloom.go:9-90` with the new API from the spec. The internal implementation (FNV-1a + DJB2 double hashing) stays the same but the constructor changes to accept `(n int, bitsPerItem int)` and internally computes `numBits = n * bitsPerItem` with a fixed `k=3` hash count.

```go
// xsearch/bloom.go
package xsearch

// Bloom is a space-efficient probabilistic filter for fast rejection.
type Bloom struct {
	bits []uint64
	size uint64
	k    int
}

const bloomHashCount = 3

// NewBloom creates a bloom filter sized for n items.
// bitsPerItem controls accuracy — higher means fewer false positives.
func NewBloom(n int, bitsPerItem int) *Bloom {
	numBits := uint64(n) * uint64(bitsPerItem)
	if numBits < 64 {
		numBits = 64
	}
	words := (numBits + 63) / 64
	return &Bloom{
		bits: make([]uint64, words),
		size: words * 64,
		k:    bloomHashCount,
	}
}

// Add inserts a key into the filter.
func (b *Bloom) Add(key string) {
	h1 := fnv1a(key)
	h2 := djb2(key)
	for i := range b.k {
		pos := (h1 + uint64(i)*h2) % b.size
		b.bits[pos/64] |= 1 << (pos % 64)
	}
}

// MayContain returns true if the key might be in the set.
// False positives are possible; false negatives are not.
func (b *Bloom) MayContain(key string) bool {
	h1 := fnv1a(key)
	h2 := djb2(key)
	for i := range b.k {
		pos := (h1 + uint64(i)*h2) % b.size
		if b.bits[pos/64]&(1<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}

// bloomSnapshot holds the serializable state of a Bloom filter.
type bloomSnapshot struct {
	Bits []uint64 `cbor:"bits"`
	Size uint64   `cbor:"size"`
	K    int      `cbor:"k"`
}

func (b *Bloom) toSnapshot() bloomSnapshot {
	return bloomSnapshot{Bits: b.bits, Size: b.size, K: b.k}
}

func bloomFromSnapshot(s bloomSnapshot) *Bloom {
	return &Bloom{bits: s.Bits, size: s.Size, k: s.K}
}

func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

func djb2(s string) uint64 {
	h := uint64(5381)
	for i := range len(s) {
		h = ((h << 5) + h) + uint64(s[i])
	}
	return h
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestBloom'`
Expected: All 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add xsearch/bloom.go xsearch/bloom_test.go
git commit -m "feat(xsearch): add bloom filter with NewBloom API"
```

---

### Task 3: N-gram Index (Field-Aware)

**Files:**
- Create: `xsearch/ngram.go`
- Create: `xsearch/ngram_test.go`

The n-gram index is ported from `index/ngram.go` but adapted:
- Works with `[]Item` instead of `[]catalog.Product`
- Tracks per-field trigrams for weighted Jaccard scoring
- `catTrigrams`/`catProducts` become generic "fallback group" structures keyed by a configurable field name
- All types unexported (internal to xsearch package)

- [ ] **Step 1: Write n-gram tests**

```go
// xsearch/ngram_test.go
package xsearch

import (
	"testing"
)

func TestExtractTrigrams(t *testing.T) {
	tests := []struct {
		input string
		want  int // expected count
	}{
		{"ab", 0},     // too short
		{"abc", 1},    // exactly one trigram
		{"budweiser", 7},
		{"", 0},
	}
	for _, tt := range tests {
		got := extractTrigrams(tt.input)
		if len(got) != tt.want {
			t.Errorf("extractTrigrams(%q): got %d trigrams, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestNgramIndex_Search(t *testing.T) {
	items := []Item{
		{ID: "budweiser", Fields: []Field{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
		{ID: "corona", Fields: []Field{
			{Name: "name", Values: []string{"Corona Extra"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
		{ID: "nike-shoe", Fields: []Field{
			{Name: "name", Values: []string{"Nike Air Max"}, Weight: 1.0},
			{Name: "category", Values: []string{"shoes"}, Weight: 0.5},
		}},
	}

	idx := newNgramIndex(items, "")
	results := idx.search("budweiser")

	if len(results) == 0 {
		t.Fatal("expected results for 'budweiser'")
	}
	if results[0].idx != 0 {
		t.Fatalf("expected first result to be index 0 (budweiser), got %d", results[0].idx)
	}
}

func TestNgramIndex_PrefixSearch(t *testing.T) {
	items := []Item{
		{ID: "bud", Fields: []Field{{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0}}},
		{ID: "cor", Fields: []Field{{Name: "name", Values: []string{"Corona"}, Weight: 1.0}}},
	}

	idx := newNgramIndex(items, "")
	results := idx.search("bu")

	if len(results) == 0 {
		t.Fatal("expected prefix match for 'bu'")
	}
}

func TestNgramIndex_WeightedScoring(t *testing.T) {
	// Item where query matches the high-weight field should score higher
	items := []Item{
		{ID: "name-match", Fields: []Field{
			{Name: "name", Values: []string{"budweiser lager"}, Weight: 1.0},
			{Name: "tags", Values: []string{"cold", "crisp"}, Weight: 0.1},
		}},
		{ID: "tag-match", Fields: []Field{
			{Name: "name", Values: []string{"corona extra"}, Weight: 1.0},
			{Name: "tags", Values: []string{"budweiser style"}, Weight: 0.1},
		}},
	}

	idx := newNgramIndex(items, "")
	results := idx.search("budweiser")

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// name-match (weight 1.0) should beat tag-match (weight 0.1)
	if results[0].idx != 0 {
		t.Fatal("expected name-match to rank first due to higher field weight")
	}
}

func TestNgramIndex_FallbackGroup(t *testing.T) {
	items := []Item{
		{ID: "a", Fields: []Field{
			{Name: "name", Values: []string{"Alpha"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
		{ID: "b", Fields: []Field{
			{Name: "name", Values: []string{"Beta"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
		{ID: "c", Fields: []Field{
			{Name: "name", Values: []string{"Gamma"}, Weight: 1.0},
			{Name: "category", Values: []string{"wine"}, Weight: 0.5},
		}},
	}

	idx := newNgramIndex(items, "category")

	group, ok := idx.bestFallbackGroup(extractTrigrams("beer"))
	if !ok {
		t.Fatal("expected to find fallback group")
	}
	if group != "beer" {
		t.Fatalf("expected group 'beer', got %q", group)
	}

	ids := idx.itemsByGroup("beer")
	if len(ids) != 2 {
		t.Fatalf("expected 2 items in beer group, got %d", len(ids))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestExtractTrigrams|TestNgramIndex'`
Expected: Compilation errors.

- [ ] **Step 3: Implement the n-gram index**

Port the core logic from `index/ngram.go` with these adaptations:
- `ngramIndex` struct (unexported) replaces `Index`
- Constructor takes `[]Item` + fallbackField string
- Per-field trigram tracking: `fieldGramCounts [][]fieldGramCount` where each document has gram counts per field with their weights
- Weighted Jaccard: `score = sum(jaccard_per_field * field.Weight)`
- Fallback group: built from the designated fallback field values instead of hardcoded `Product.Category`
- `ngramSearchResult` struct: `{idx int, score float64}` (internal index, not string ID)
- `search(query string) []ngramSearchResult` — full search with prefix fallback for short queries
- `searchWithGrams(queryGrams []string) []ngramSearchResult` — Jaccard search with pre-extracted trigrams
- `bestFallbackGroup(queryGrams []string) (string, bool)` — replaces `BestCategoryWithGrams`
- `itemsByGroup(group string) []int` — replaces `ProductsByCategory`

Key internal types:

```go
type fieldGramCount struct {
	fieldIdx int
	weight   float64
	count    int
}

type ngramSearchResult struct {
	idx   int     // index into items slice
	score float64
}
```

The implementation follows the same pooled hit-counting pattern from `index/ngram.go:245-319` but computes weighted scores per field. The sorted-names prefix search from `index/ngram.go:323-339` is adapted to use the highest-weighted field's values.

Reference implementations to port:
- `extractTrigrams`: `index/ngram.go:25-43` (normalize + extract 3-grams)
- `normalizeQuery`: `index/ngram.go:45-65` (trim + lowercase)
- `searchWithGrams`: `index/ngram.go:245-319` (pooled Jaccard scoring — adapt for per-field weights)
- `prefixSearch`: `index/ngram.go:323-339` (binary search on sorted names)
- `bestCategoryWithGrams` -> `bestFallbackGroup`: `index/ngram.go:425-443`
- `countIntersection`: `index/ngram.go:445-459`

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestExtractTrigrams|TestNgramIndex'`
Expected: All 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add xsearch/ngram.go xsearch/ngram_test.go
git commit -m "feat(xsearch): add field-aware n-gram index with weighted Jaccard scoring"
```

---

### Task 4: BM25 Index (Per-Field Scoring)

**Files:**
- Create: `xsearch/bm25.go`
- Create: `xsearch/bm25_test.go`

Port from `bm25/bm25.go` with per-field scoring. Currently BM25 concatenates all fields into one document. The new version scores each field independently and multiplies by weight.

- [ ] **Step 1: Write BM25 tests**

```go
// xsearch/bm25_test.go
package xsearch

import "testing"

func TestBM25Tokenize(t *testing.T) {
	tokens := bm25Tokenize("Hello World! 123")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "hello" {
		t.Fatalf("expected 'hello', got %q", tokens[0])
	}
}

func TestBM25Index_Search(t *testing.T) {
	items := []Item{
		{ID: "budweiser", Fields: []Field{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
		{ID: "corona", Fields: []Field{
			{Name: "name", Values: []string{"Corona Extra"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}},
	}

	idx := newBM25Index(items, 1.2, 0.75)
	results := idx.search("budweiser")

	if len(results) == 0 {
		t.Fatal("expected results for 'budweiser'")
	}
	if results[0].idx != 0 {
		t.Fatalf("expected first result index 0, got %d", results[0].idx)
	}
}

func TestBM25Index_PerFieldWeighting(t *testing.T) {
	items := []Item{
		{ID: "name-match", Fields: []Field{
			{Name: "name", Values: []string{"budweiser lager"}, Weight: 1.0},
			{Name: "tags", Values: []string{"american"}, Weight: 0.1},
		}},
		{ID: "tag-match", Fields: []Field{
			{Name: "name", Values: []string{"corona extra"}, Weight: 1.0},
			{Name: "tags", Values: []string{"budweiser style"}, Weight: 0.1},
		}},
	}

	idx := newBM25Index(items, 1.2, 0.75)
	results := idx.search("budweiser")

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// name-match should score higher because "budweiser" matches the name field (weight 1.0)
	if results[0].idx != 0 {
		t.Fatal("expected name-match to rank first")
	}
	if results[0].score <= results[1].score {
		t.Fatal("expected name-match to have higher score")
	}
}

func TestBM25Index_PrefixBoost(t *testing.T) {
	items := []Item{
		{ID: "bud-direct", Fields: []Field{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
		}},
		{ID: "bud-mention", Fields: []Field{
			{Name: "name", Values: []string{"Funky Buddha Floridian"}, Weight: 1.0},
		}},
	}

	idx := newBM25Index(items, 1.2, 0.75)
	results := idx.search("bud")

	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// "Budweiser" starts with "bud" → prefix boost → should rank first
	if results[0].idx != 0 {
		t.Fatal("expected Budweiser to rank first due to prefix boost")
	}
}

func TestBM25Index_MultiValueField(t *testing.T) {
	items := []Item{
		{ID: "drink", Fields: []Field{
			{Name: "name", Values: []string{"Margarita"}, Weight: 1.0},
			{Name: "ingredients", Values: []string{"tequila", "lime", "triple sec"}, Weight: 0.4},
		}},
	}

	idx := newBM25Index(items, 1.2, 0.75)
	results := idx.search("tequila")

	if len(results) == 0 {
		t.Fatal("expected match on ingredient")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestBM25'`
Expected: Compilation errors.

- [ ] **Step 3: Implement the BM25 index**

Port from `bm25/bm25.go` with these key changes:
- `bm25Index` struct (unexported) replaces `Index`
- Constructor `newBM25Index(items []Item, k1, b float64)` takes `[]Item`
- Per-field scoring: each field is a separate "sub-document". BM25 score per field is computed independently, then `totalScore = sum(bm25_field_i * field_i.Weight)`
- Prefix boosting: only extracted from the highest-weighted field (primary field), same logic as `bm25.go:166-187`
- `bm25SearchResult` struct: `{idx int, score float64, prefixMatch bool}`
- Same pooled candidate/heap pattern from `bm25.go:252-354`
- `bm25Tokenize()` replaces exported `Tokenize()`: same logic from `bm25.go:85-94`

Internal structure per document:

```go
type bm25FieldData struct {
	termFreqs []bm25TermFreq
	docLen    int
	weight    float64
}
```

The index stores `[][]bm25FieldData` (doc index -> fields). IDF is computed globally across all fields. Posting lists map terms to document indices (same as before). The `score()` method iterates fields and sums `bm25Score(field) * field.weight`.

Reference implementations to port:
- `Tokenize`: `bm25/bm25.go:85-94`
- `NewIndex`: `bm25/bm25.go:121-204` (adapt loop from Product to Item/Fields)
- `Score`: `bm25/bm25.go:208-232` (adapt for per-field)
- `HasPrefixMatch`: `bm25/bm25.go:237-248`
- `Search`: `bm25/bm25.go:252-354` (heap pattern stays same)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestBM25'`
Expected: All 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add xsearch/bm25.go xsearch/bm25_test.go
git commit -m "feat(xsearch): add per-field BM25 index with prefix boosting"
```

---

### Task 5: Engine Search Pipeline

**Files:**
- Modify: `xsearch/xsearch.go`
- Modify: `xsearch/xsearch_test.go`

Wire the bloom filter, n-gram index, and BM25 index into `Engine.Search()`. Port the search pipeline from `engine/engine.go:236-350`.

- [ ] **Step 1: Write search integration tests**

Add to `xsearch/xsearch_test.go`:

```go
func makeTestItems() []Searchable {
	return []Searchable{
		testItem{id: "budweiser", fields: []Field{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
			{Name: "tags", Values: []string{"lager", "american"}, Weight: 0.3},
		}},
		testItem{id: "corona", fields: []Field{
			{Name: "name", Values: []string{"Corona Extra"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
			{Name: "tags", Values: []string{"lager", "mexican"}, Weight: 0.3},
		}},
		testItem{id: "nike-air", fields: []Field{
			{Name: "name", Values: []string{"Nike Air Max"}, Weight: 1.0},
			{Name: "category", Values: []string{"shoes"}, Weight: 0.5},
		}},
		testItem{id: "heineken", fields: []Field{
			{Name: "name", Values: []string{"Heineken"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
			{Name: "tags", Values: []string{"lager", "dutch"}, Weight: 0.3},
		}},
	}
}

func TestEngine_Search_DirectMatch(t *testing.T) {
	eng, err := New(makeTestItems())
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results for 'budweiser'")
	}
	if results[0].ID != "budweiser" {
		t.Fatalf("expected first result 'budweiser', got %q", results[0].ID)
	}
	if results[0].MatchType != MatchDirect {
		t.Fatal("expected MatchDirect")
	}
}

func TestEngine_Search_EmptyQuery(t *testing.T) {
	eng, err := New(makeTestItems())
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("")
	if len(results) != 0 {
		t.Fatal("expected no results for empty query")
	}
}

func TestEngine_Search_FuzzyMatch(t *testing.T) {
	eng, err := New(makeTestItems())
	if err != nil {
		t.Fatal(err)
	}

	// Typo: "budwiser" should still find "Budweiser" via Jaccard
	results := eng.Search("budwiser")
	if len(results) == 0 {
		t.Fatal("expected fuzzy match for 'budwiser'")
	}
	if results[0].ID != "budweiser" {
		t.Fatalf("expected 'budweiser', got %q", results[0].ID)
	}
}

func TestEngine_Search_WithBloom(t *testing.T) {
	eng, err := New(makeTestItems(), WithBloom(100))
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results with bloom enabled")
	}

	// Nonsense query should be rejected by bloom
	results = eng.Search("xzqwvp")
	if len(results) != 0 {
		t.Fatal("expected bloom to reject nonsense query")
	}
}

func TestEngine_Search_WithLimit(t *testing.T) {
	eng, err := New(makeTestItems(), WithLimit(2))
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("lager")
	if len(results) > 2 {
		t.Fatalf("expected at most 2 results, got %d", len(results))
	}
}

func TestEngine_Search_WithFallback(t *testing.T) {
	eng, err := New(makeTestItems(), WithFallbackField("category"))
	if err != nil {
		t.Fatal(err)
	}

	// Search for category name — should trigger fallback
	results := eng.Search("beer")
	if len(results) == 0 {
		t.Fatal("expected fallback results for 'beer'")
	}
	// At least some results should exist (direct or fallback)
	hasBeer := false
	for _, r := range results {
		item, _ := eng.Get(r.ID)
		for _, f := range item.Fields {
			if f.Name == "category" && len(f.Values) > 0 && f.Values[0] == "beer" {
				hasBeer = true
			}
		}
	}
	if !hasBeer {
		t.Fatal("expected beer items in results")
	}
}

type fixedScorer struct {
	scores map[string]float64
}

func (s fixedScorer) Score(id string) float64 { return s.scores[id] }

func TestEngine_Search_WithScorer(t *testing.T) {
	scorer := fixedScorer{scores: map[string]float64{
		"corona": 1.0, // boost corona
	}}

	eng, err := New(makeTestItems(), WithScorer(scorer), WithAlpha(0.5))
	if err != nil {
		t.Fatal(err)
	}

	// Both are "beer" tagged with "lager", but corona has a popularity boost
	results := eng.Search("lager")
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// Find corona's position — should be near the top due to scorer boost
	for _, r := range results {
		if r.ID == "corona" && r.Score > 0 {
			return // corona found with positive score, good
		}
	}
	t.Fatal("expected corona to appear in results with scorer boost")
}

func TestEngine_Search_ScorerClampsNegative(t *testing.T) {
	scorer := fixedScorer{scores: map[string]float64{
		"budweiser": -5.0, // negative should be clamped to 0
	}}

	eng, err := New(makeTestItems(), WithScorer(scorer), WithAlpha(0.5))
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// Score should not be negative
	if results[0].Score < 0 {
		t.Fatalf("score should not be negative, got %f", results[0].Score)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestEngine_Search'`
Expected: Tests fail — `Search()` returns nil.

- [ ] **Step 3: Implement Engine.Search()**

Update `xsearch/xsearch.go`:
- Add `ngramIdx *ngramIndex`, `bm25Idx *bm25Index`, `bloom *Bloom` fields to `Engine`
- In `New()`, after validation: build n-gram index, BM25 index, and optionally bloom filter
- Implement `Search()` following the pipeline from `engine/engine.go:236-350`:
  1. Normalize query (port `normalizeQuery` from `engine/engine.go:659-679`)
  2. Extract trigrams
  3. If bloom enabled: check trigrams against bloom, skip if all miss
  4. Try BM25 first (primary path)
  5. Fallback to Jaccard n-gram search
  6. If few direct results and fallbackField set: add fallback group results
  7. Score with external Scorer if configured: per-search max-normalization, clamp negatives/NaN/Inf
  8. Sort by combined score, truncate to limit
  9. Compute highlights on final results

Highlight computation: port from `engine/engine.go:372-397` but adapt for multi-value fields. For each result, iterate the item's fields, check which field values contain query words, and record `Highlight{Start, End, ValueIndex}`.

Key helper to port: `normalizeQuery` from `engine/engine.go:659-679`, `extractNormalizedTrigrams` from `engine/engine.go:690-704`, `forEachQueryWord` from `engine/engine.go:706-720`.

Scorer integration: collect `Scorer.Score(id)` for all candidates, clamp negatives to 0, handle NaN/Inf by treating as 0. Find max, normalize to [0,1]. Blend: `final = (1-alpha)*relevance + alpha*normalizedScore`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestEngine_Search'`
Expected: All 8 PASS.

- [ ] **Step 5: Commit**

```bash
git add xsearch/xsearch.go xsearch/xsearch_test.go
git commit -m "feat(xsearch): implement Engine.Search() with full pipeline"
```

---

### Task 6: Highlights for Multi-Value Fields

**Files:**
- Modify: `xsearch/xsearch.go` (highlight logic)
- Modify: `xsearch/xsearch_test.go`

- [ ] **Step 1: Write highlight tests**

Add to `xsearch/xsearch_test.go`:

```go
func TestEngine_Search_Highlights(t *testing.T) {
	eng, err := New([]Searchable{
		testItem{id: "bud", fields: []Field{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("bud")
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	hl, ok := results[0].Highlights["name"]
	if !ok || len(hl) == 0 {
		t.Fatal("expected highlights on name field")
	}
	if hl[0].Start != 0 || hl[0].End != 3 {
		t.Fatalf("expected highlight [0,3), got [%d,%d)", hl[0].Start, hl[0].End)
	}
	if hl[0].ValueIndex != 0 {
		t.Fatalf("expected ValueIndex 0, got %d", hl[0].ValueIndex)
	}
}

func TestEngine_Search_MultiValueHighlights(t *testing.T) {
	eng, err := New([]Searchable{
		testItem{id: "drink", fields: []Field{
			{Name: "name", Values: []string{"Margarita"}, Weight: 1.0},
			{Name: "ingredients", Values: []string{"tequila", "lime juice", "triple sec"}, Weight: 0.4},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	results := eng.Search("tequila")
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	hl, ok := results[0].Highlights["ingredients"]
	if !ok || len(hl) == 0 {
		t.Fatal("expected highlights on ingredients field")
	}
	// "tequila" is Values[0], so ValueIndex should be 0
	if hl[0].ValueIndex != 0 {
		t.Fatalf("expected ValueIndex 0, got %d", hl[0].ValueIndex)
	}
	if hl[0].Start != 0 || hl[0].End != 7 {
		t.Fatalf("expected highlight [0,7) for 'tequila', got [%d,%d)", hl[0].Start, hl[0].End)
	}
}
```

- [ ] **Step 2: Run tests — may pass if highlights were implemented in Task 5, may fail if deferred**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestEngine_Search_.*Highlight'`

- [ ] **Step 3: Implement/fix multi-value highlight logic**

In the highlight computation, for each field in the matched item:
```go
for _, field := range item.Fields {
    for vi, value := range field.Values {
        lowerValue := strings.ToLower(value)
        // Check each query word against this value
        forEachQueryWord(query, func(word string) {
            pos := strings.Index(lowerValue, word)
            if pos >= 0 {
                highlights[field.Name] = append(highlights[field.Name], Highlight{
                    Start:      pos,
                    End:        pos + len(word),
                    ValueIndex: vi,
                })
            }
        })
    }
}
```

Then merge overlapping highlights per (fieldName, valueIndex) pair — port `mergeHighlights` from `engine/engine.go:400-418`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestEngine_Search_.*Highlight'`
Expected: Both PASS.

- [ ] **Step 5: Commit**

```bash
git add xsearch/xsearch.go xsearch/xsearch_test.go
git commit -m "feat(xsearch): add multi-value field highlights with ValueIndex"
```

---

### Task 7: CBOR Snapshot Support

**Files:**
- Create: `xsearch/snapshot.go`
- Create: `xsearch/snapshot_test.go`

- [ ] **Step 1: Write snapshot tests**

```go
// xsearch/snapshot_test.go
package xsearch

import (
	"testing"
)

func TestSnapshot_RoundTrip(t *testing.T) {
	items := makeTestItems()
	eng, err := New(items, WithBloom(100), WithBM25(1.2, 0.75), WithFallbackField("category"))
	if err != nil {
		t.Fatal(err)
	}

	data, err := eng.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	eng2, err := NewFromSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}

	// Same search should produce same results
	r1 := eng.Search("budweiser")
	r2 := eng2.Search("budweiser")

	if len(r1) != len(r2) {
		t.Fatalf("result count mismatch: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].ID != r2[i].ID {
			t.Fatalf("result %d ID mismatch: %q vs %q", i, r1[i].ID, r2[i].ID)
		}
	}
}

func TestSnapshot_GetWorksAfterReload(t *testing.T) {
	items := makeTestItems()
	eng, err := New(items)
	if err != nil {
		t.Fatal(err)
	}

	data, err := eng.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	eng2, err := NewFromSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}

	item, ok := eng2.Get("budweiser")
	if !ok {
		t.Fatal("expected to find item after snapshot reload")
	}
	if item.Fields[0].Values[0] != "Budweiser" {
		t.Fatalf("expected 'Budweiser', got %q", item.Fields[0].Values[0])
	}
}

func TestSnapshot_VersionHeader(t *testing.T) {
	items := makeTestItems()
	eng, err := New(items)
	if err != nil {
		t.Fatal(err)
	}

	data, err := eng.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	// Check magic bytes
	if string(data[:4]) != "XSRC" {
		t.Fatalf("expected magic 'XSRC', got %q", string(data[:4]))
	}
}

func TestSnapshot_RejectsBadMagic(t *testing.T) {
	_, err := NewFromSnapshot([]byte("BADXxxxxxxxx"))
	if err == nil {
		t.Fatal("expected error for bad magic bytes")
	}
}

func TestSnapshot_RejectsEmptyData(t *testing.T) {
	_, err := NewFromSnapshot(nil)
	if err == nil {
		t.Fatal("expected error for nil data")
	}
}

func TestSnapshot_RuntimeOptionsOverride(t *testing.T) {
	items := makeTestItems()
	scorer := fixedScorer{scores: map[string]float64{"budweiser": 1.0}}

	eng, err := New(items, WithBloom(100))
	if err != nil {
		t.Fatal(err)
	}

	data, err := eng.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	// Override scorer and alpha at load time
	eng2, err := NewFromSnapshot(data, WithScorer(scorer), WithAlpha(0.8), WithLimit(5))
	if err != nil {
		t.Fatal(err)
	}

	results := eng2.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results")
	}
}

func TestSnapshot_RejectsBuildTimeOptionAtLoadTime(t *testing.T) {
	items := makeTestItems()
	eng, err := New(items)
	if err != nil {
		t.Fatal(err)
	}

	data, err := eng.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	// WithBloom is a build-time option — should error on NewFromSnapshot
	_, err = NewFromSnapshot(data, WithBloom(100))
	if err == nil {
		t.Fatal("expected error when passing build-time option to NewFromSnapshot")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestSnapshot'`
Expected: Compilation errors — `NewFromSnapshot` and `Snapshot` not implemented.

- [ ] **Step 3: Implement snapshot**

```go
// xsearch/snapshot.go
package xsearch

import (
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

var snapshotMagic = [4]byte{'X', 'S', 'R', 'C'}

const snapshotVersion uint8 = 1

// snapshotPayload is the CBOR-serializable snapshot.
type snapshotPayload struct {
	Items         []Item             `cbor:"items"`
	NgramSnap     ngramSnapshot      `cbor:"ngram"`
	BM25Snap      bm25Snapshot       `cbor:"bm25"`
	BloomSnap     *bloomSnapshot     `cbor:"bloom,omitempty"`
	BM25K1        float64            `cbor:"bm25_k1"`
	BM25B         float64            `cbor:"bm25_b"`
	BloomBPI      int                `cbor:"bloom_bpi"`
	FallbackField string             `cbor:"fallback_field,omitempty"`
	Limit         int                `cbor:"limit"`
}
```

`Snapshot()` method on Engine:
1. Write magic bytes + version byte
2. Serialize `snapshotPayload` to CBOR using the internal snapshot types from ngram and bm25
3. Return `magic + version + cbor_bytes`

`NewFromSnapshot(data, opts)`:
1. Check `len(data) >= 5`, verify magic bytes, check version
2. CBOR decode the payload from `data[5:]`
3. Rebuild `Engine` from snapshot: items, n-gram index from snapshot, BM25 from snapshot, bloom from snapshot
4. Apply only runtime options (`WithScorer`, `WithAlpha`, `WithLimit`). Return error if build-time options are passed.

To distinguish build-time vs runtime options, add a `buildTime bool` flag to the option function or use a separate option type:

```go
type Option struct {
	apply    func(*engineConfig)
	buildTime bool
}

func WithBloom(bitsPerItem int) Option {
	return Option{
		apply:    func(c *engineConfig) { c.bloomBPI = bitsPerItem },
		buildTime: true,
	}
}

func WithScorer(s Scorer) Option {
	return Option{
		apply:    func(c *engineConfig) { c.scorer = s },
		buildTime: false,
	}
}
```

This requires updating the `Option` type from a function to a struct — update `config.go` accordingly.

The internal snapshot types (`ngramSnapshot`, `bm25Snapshot`) are CBOR-tagged structs that mirror the index state. Port the snapshot/from-snapshot logic:
- N-gram: `index/ngram.go:98-156` (Snapshot struct + ToSnapshot + FromSnapshot)
- BM25: `bm25/bm25.go:357-409` (Snapshot struct + ToSnapshot + FromSnapshot)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v -run 'TestSnapshot'`
Expected: All 7 PASS.

- [ ] **Step 5: Run full library test suite**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -v`
Expected: All tests PASS (validation, Get, bloom, ngram, bm25, search, highlights, snapshots).

- [ ] **Step 6: Commit**

```bash
git add xsearch/snapshot.go xsearch/snapshot_test.go xsearch/config.go
git commit -m "feat(xsearch): add self-contained CBOR snapshots with version header"
```

---

### Task 8: Library Benchmarks

**Files:**
- Create: `xsearch/bench_test.go`

- [ ] **Step 1: Write library benchmarks**

```go
// xsearch/bench_test.go
package xsearch

import "testing"

func benchItems() []Searchable {
	// Generate a realistic-sized dataset for benchmarking
	names := []string{
		"Budweiser", "Corona Extra", "Heineken", "Stella Artois",
		"Guinness Draught", "Blue Moon", "Sierra Nevada Pale Ale",
		"Samuel Adams Boston Lager", "Lagunitas IPA", "Dogfish Head 60 Minute",
	}
	categories := []string{"beer", "beer", "beer", "beer", "beer", "beer", "beer", "beer", "beer", "beer"}
	tags := [][]string{
		{"lager", "american"}, {"lager", "mexican"}, {"lager", "dutch"},
		{"lager", "belgian"}, {"stout", "irish"}, {"wheat", "craft"},
		{"pale ale", "craft"}, {"lager", "craft"}, {"ipa", "craft"}, {"ipa", "craft"},
	}

	items := make([]Searchable, len(names))
	for i, name := range names {
		tagVals := tags[i]
		items[i] = testItem{
			id: name,
			fields: []Field{
				{Name: "name", Values: []string{name}, Weight: 1.0},
				{Name: "category", Values: []string{categories[i]}, Weight: 0.5},
				{Name: "tags", Values: tagVals, Weight: 0.3},
			},
		}
	}
	return items
}

func BenchmarkNew(b *testing.B) {
	items := benchItems()
	b.ResetTimer()
	for b.Loop() {
		_, _ = New(items, WithBloom(100), WithFallbackField("category"))
	}
}

func BenchmarkSearch_Direct(b *testing.B) {
	eng, _ := New(benchItems(), WithBloom(100), WithFallbackField("category"))
	b.ResetTimer()
	for b.Loop() {
		eng.Search("budweiser")
	}
}

func BenchmarkSearch_Fuzzy(b *testing.B) {
	eng, _ := New(benchItems(), WithBloom(100), WithFallbackField("category"))
	b.ResetTimer()
	for b.Loop() {
		eng.Search("budwiser")
	}
}

func BenchmarkSearch_Prefix(b *testing.B) {
	eng, _ := New(benchItems(), WithBloom(100), WithFallbackField("category"))
	b.ResetTimer()
	for b.Loop() {
		eng.Search("bu")
	}
}

func BenchmarkSearch_BloomReject(b *testing.B) {
	eng, _ := New(benchItems(), WithBloom(100), WithFallbackField("category"))
	b.ResetTimer()
	for b.Loop() {
		eng.Search("xzqwvp")
	}
}

func BenchmarkSnapshot(b *testing.B) {
	eng, _ := New(benchItems(), WithBloom(100), WithFallbackField("category"))
	b.ResetTimer()
	for b.Loop() {
		_, _ = eng.Snapshot()
	}
}

func BenchmarkNewFromSnapshot(b *testing.B) {
	eng, _ := New(benchItems(), WithBloom(100), WithFallbackField("category"))
	data, _ := eng.Snapshot()
	b.ResetTimer()
	for b.Loop() {
		_, _ = NewFromSnapshot(data)
	}
}
```

- [ ] **Step 2: Run benchmarks**

Run: `cd /home/mbow/code/search && go test ./xsearch/ -bench=. -benchmem -count=1`
Expected: All benchmarks run and report results.

- [ ] **Step 3: Commit**

```bash
git add xsearch/bench_test.go
git commit -m "test(xsearch): add library benchmarks"
```

---

### Task 9: Adapt catalog Package

**Files:**
- Modify: `catalog/catalog.go`
- Modify: `catalog/embed.go`
- Modify: `catalog/catalog_test.go`
- Modify: `catalog/embed_test.go`

- [ ] **Step 1: Write Searchable interface tests for Product**

Add to `catalog/catalog_test.go`:

```go
func TestProduct_SearchID(t *testing.T) {
	p := Product{Name: "Budweiser", Category: "beer"}
	id := p.SearchID()
	if id == "" {
		t.Fatal("expected non-empty SearchID")
	}
}

func TestProduct_SearchFields(t *testing.T) {
	p := Product{Name: "Budweiser", Category: "beer", Tags: []string{"lager", "american"}}
	fields := p.SearchFields()
	if len(fields) == 0 {
		t.Fatal("expected fields")
	}

	// Check name field exists with weight > 0
	found := false
	for _, f := range fields {
		if f.Name == "name" && f.Values[0] == "Budweiser" && f.Weight > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected name field")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./catalog/ -v -run 'TestProduct_Search'`
Expected: Compilation error — methods don't exist.

- [ ] **Step 3: Add SearchID and SearchFields to Product**

In `catalog/catalog.go`:

```go
import (
	"strconv"
	"github.com/mbow/go-xsearch/xsearch"
)

// SearchID returns a stable string identifier for the product.
// Uses the product name slugified as the ID. For the current dataset
// where names are unique, this is sufficient.
func (p Product) SearchID() string {
	return slugify(p.Name)
}

// SearchFields returns the searchable fields for this product.
func (p Product) SearchFields() []xsearch.Field {
	fields := []xsearch.Field{
		{Name: "name", Values: []string{p.Name}, Weight: 1.0},
		{Name: "category", Values: []string{p.Category}, Weight: 0.5},
	}
	if len(p.Tags) > 0 {
		fields = append(fields, xsearch.Field{Name: "tags", Values: p.Tags, Weight: 0.3})
	}
	return fields
}

func slugify(s string) string {
	// Simple slug: lowercase, replace spaces with hyphens
	b := make([]byte, 0, len(s))
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b = append(b, c+('a'-'A'))
		case c == ' ' || c == '\t':
			b = append(b, '-')
		default:
			b = append(b, c)
		}
	}
	return string(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./catalog/ -v -run 'TestProduct_Search'`
Expected: Both PASS.

- [ ] **Step 5: Simplify embed.go for snapshot-based loading**

Update `catalog/embed.go`: The embedded CBOR still stores products for the sample app to reference, but it now also stores a self-contained xsearch snapshot. The `EmbeddedBloomRaw`, `EmbeddedIndexRaw`, `EmbeddedBM25Raw` functions are replaced with `EmbeddedSnapshot() ([]byte, error)` which returns the xsearch snapshot bytes.

This change depends on Task 12 (cmd/generate update), so for now add the new function and keep the old ones. They'll be removed in Task 13.

```go
// EmbeddedSnapshot returns the raw xsearch snapshot bytes from the embedded data.
func EmbeddedSnapshot() ([]byte, error) {
	initEmbedded()
	return []byte(decoded.SnapshotRaw), initErr
}
```

Add `SnapshotRaw cbor.RawMessage` to the `payload` struct.

- [ ] **Step 6: Commit**

```bash
git add catalog/catalog.go catalog/catalog_test.go catalog/embed.go
git commit -m "feat(catalog): implement Searchable interface on Product"
```

---

### Task 10: Adapt ranking Package

**Files:**
- Modify: `ranking/ranking.go`
- Modify: `ranking/ranking_test.go`

- [ ] **Step 1: Write Scorer interface test**

Add to `ranking/ranking_test.go`:

```go
func TestRanker_ScoreStringID(t *testing.T) {
	r := New(0.05, 0.6)
	r.RecordSelection("beer-1")
	r.RecordSelection("beer-1")

	score := r.Score("beer-1")
	if score <= 0 {
		t.Fatal("expected positive score for selected item")
	}

	score = r.Score("unknown")
	if score != 0 {
		t.Fatalf("expected 0 for unknown item, got %f", score)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v -run 'TestRanker_ScoreStringID'`
Expected: Compilation error.

- [ ] **Step 3: Migrate ranking to string IDs**

Key changes to `ranking/ranking.go`:
- `selections map[int][]time.Time` becomes `selections map[string][]time.Time`
- Remove `numProducts int` from constructor (no longer needed — map-based, not slice-based)
- `RecordSelection(productID int)` becomes `RecordSelection(id string)`
- `SetSelections(productID int, ...)` becomes `SetSelections(id string, ...)`
- `rawScore(productID int, now)` becomes `rawScore(id string, now)`
- Add `Score(id string) float64` method that satisfies `xsearch.Scorer`
- `ScoreView` internal `scores` changes from `[]float64` to `map[string]float64`
- `rebuildSnapshotLocked()`: iterate `selections` map, build `map[string]float64` normalized scores
- `Save`/`Load`: JSON format changes from `map[int][]time.Time` to `map[string][]time.Time`
- `Prune()`: same logic, just string keys
- `New(lambda, alpha)` — drop `numProducts` parameter

The `ScoreView.Score(productID int, relevance float64)` method used by the old engine is removed. The new `Score(id string) float64` method on `Ranker` directly satisfies `xsearch.Scorer`.

- [ ] **Step 4: Update existing ranking tests for string IDs**

All existing tests in `ranking/ranking_test.go` need int IDs replaced with string IDs. For example, `RecordSelection(0)` becomes `RecordSelection("0")`, `CombinedScore(0, 0.8)` becomes `Score("0")`, etc.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v`
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add ranking/ranking.go ranking/ranking_test.go
git commit -m "refactor(ranking): migrate to string IDs, implement xsearch.Scorer"
```

---

### Task 11: Adapt Server

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: Update server to use xsearch.Engine**

Key changes to `internal/server/server.go`:
- Replace `engine` import with `xsearch`
- `App.Engine` type changes from `*engine.Engine` to `*xsearch.Engine`
- Remove `productCount int` — validate IDs via `engine.Get(id)` instead
- `New()` takes `*xsearch.Engine` and `*ranking.Ranker` (separate, since ranking is no longer inside the engine)
- Add `App.Ranker *ranking.Ranker` field for selection recording

`renderResultsFragment()`:
- Uses `xsearch.Result` instead of `engine.Result`
- Checks `res.MatchType != xsearch.MatchDirect` etc.
- Calls `app.Engine.Get(res.ID)` to get item data for rendering

`writeResultItem()`:
- Uses string ID in `hx-vals`: `hx-vals='{"id": "` + res.ID + `"}'`
- Builds highlighted name from `res.Highlights` + `item.Fields` (the "name" field):
  - Get item via `app.Engine.Get(res.ID)`
  - Find the "name" field
  - Apply highlights to build `template.HTML` — port `buildHighlightedName` logic from `engine/engine.go:432-454`

`HandleSelect()`:
- Remove `strconv.Atoi` — ID is already a string
- Validate: `_, ok := app.Engine.Get(idStr); if !ok { 400 }`
- Call `app.Ranker.RecordSelection(idStr)` instead of `app.Engine.RecordSelection(id)`

`ghostSuffix()`:
- Get first result's name via `app.Engine.Get(results[0].ID)` instead of `results[0].Product.Name`

- [ ] **Step 2: Update server tests**

Update `internal/server/server_test.go`:
- Build engine via `xsearch.New()` with catalog products as `Searchable`
- Create `ranking.Ranker` separately
- Pass both to `server.New()`
- Update select handler tests to use string IDs

- [ ] **Step 3: Run server tests**

Run: `cd /home/mbow/code/search && go test ./internal/server/ -v`
Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "refactor(server): use xsearch.Engine with string IDs"
```

---

### Task 12: Adapt cmd/generate

**Files:**
- Modify: `cmd/generate/main.go`

- [ ] **Step 1: Rewrite generate to use xsearch**

The generator now:
1. Reads `data/products.json`
2. Converts to `[]xsearch.Searchable` via `catalog.Product`
3. Calls `xsearch.New(items, WithBloom(100), WithBM25(1.2, 0.75), WithFallbackField("category"))`
4. Calls `eng.Snapshot()` to get the self-contained CBOR blob
5. Writes a payload containing both the products (for sample app rendering) and the xsearch snapshot

```go
type Payload struct {
	Products    []catalog.Product `cbor:"products"`
	SnapshotRaw cbor.RawMessage   `cbor:"snapshot"`
}
```

Port from `cmd/generate/main.go:50-142`, replacing the separate bloom/index/bm25 build with a single `xsearch.New()` + `Snapshot()` call.

- [ ] **Step 2: Run generate and verify output**

Run: `cd /home/mbow/code/search && go run ./cmd/generate/`
Expected: Generates `catalog/data.cbor` successfully.

- [ ] **Step 3: Commit**

```bash
git add cmd/generate/main.go
git commit -m "refactor(generate): use xsearch.New() + Snapshot() for CBOR generation"
```

---

### Task 13: Adapt main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Update main.go to use xsearch**

Replace the current initialization flow:
```go
// Old: 4 separate embedded loads + engine.NewFromEmbedded
// New: single snapshot load + xsearch.NewFromSnapshot

products, err := catalog.EmbeddedProducts()  // still needed for sample app rendering
snapshotRaw, err := catalog.EmbeddedSnapshot()

ranker := ranking.New(0.05, 0.6)
ranker.Load(popPath)
ranker.Prune(90)

eng, err := xsearch.NewFromSnapshot(snapshotRaw,
    xsearch.WithScorer(ranker),
    xsearch.WithAlpha(0.6),
)

app := server.New(eng, ranker, 1024)
```

The rest of main.go (signal handling, periodic snapshot, graceful shutdown) stays the same, just using `ranker` directly instead of `app.Engine.Ranker()`.

- [ ] **Step 2: Build and verify the app starts**

Run: `cd /home/mbow/code/search && go build -o /dev/null .`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "refactor(main): use xsearch.NewFromSnapshot with Scorer"
```

---

### Task 14: Update Benchmarks

**Files:**
- Modify: `benchmarks/suite_test.go`

- [ ] **Step 1: Update benchmark imports and engine construction**

Replace imports of `bloom`, `index`, `bm25`, `engine` with `xsearch`. Update `benchEngine()` to use `xsearch.NewFromSnapshot()`. Update all engine-level benchmarks to use `xsearch.Engine`. Keep benchmark names and queries identical for regression comparison.

Key changes:
- `benchEngine()`: load embedded snapshot, call `xsearch.NewFromSnapshot()`
- Engine benchmarks: `e.Search("bud")` stays the same (same API)
- `RecordSelection(0)` becomes `RecordSelection("budweiser")` or similar string ID
- Server benchmarks: update `server.New()` to take `*xsearch.Engine` + `*ranking.Ranker`
- Bloom benchmarks: use `xsearch.NewBloom()` instead of `bloom.New()`
- Index benchmarks using `index.NewIndex()` and `index.ExtractTrigrams()`: these reference the old package. Since `xsearch` internals are unexported, these need to either use the `xsearch.Engine` API or be temporarily removed until the old packages are deleted. The pragmatic approach: keep them as engine-level benchmarks through `xsearch.Engine.Search()` which exercises the same code paths.

- [ ] **Step 2: Run benchmarks**

Run: `cd /home/mbow/code/search && go test ./benchmarks/ -bench=. -benchmem -count=1 -timeout 120s`
Expected: All benchmarks run. Save output to compare later.

- [ ] **Step 3: Commit**

```bash
git add benchmarks/suite_test.go
git commit -m "refactor(benchmarks): update to use xsearch library"
```

---

### Task 15: Regenerate Embedded Data

**Files:**
- Modify: `catalog/data.cbor` (regenerated)
- Modify: `catalog/embed.go` (remove old functions)

- [ ] **Step 1: Regenerate the embedded CBOR**

Run: `cd /home/mbow/code/search && go generate ./catalog/`

Update the `//go:generate` directive in `catalog/embed.go` if the generate command changed.

- [ ] **Step 2: Remove old embed functions**

Remove from `catalog/embed.go`:
- `EmbeddedBloomRaw()`
- `EmbeddedIndexRaw()`
- `EmbeddedBM25Raw()`
- Old `BloomRaw`, `IndexRaw`, `BM25Raw` fields from the `payload` struct

Keep:
- `EmbeddedProducts()` — still needed by sample app for rendering
- `EmbeddedSnapshot()` — new function
- `GetByName()`, `GetByID()` — still useful for sample app

- [ ] **Step 3: Verify everything still compiles and tests pass**

Run: `cd /home/mbow/code/search && go test ./... 2>&1 | tail -20`
Expected: All packages compile and tests pass (old packages may have failing tests at this point since their consumers changed — that's expected and resolved in Task 16).

- [ ] **Step 4: Commit**

```bash
git add catalog/
git commit -m "refactor(catalog): regenerate embedded data with xsearch snapshot"
```

---

### Task 16: Delete Old Packages

**Files:**
- Delete: `bloom/`
- Delete: `index/`
- Delete: `bm25/`
- Delete: `engine/`

- [ ] **Step 1: Verify no remaining imports of old packages**

Run: `cd /home/mbow/code/search && grep -r '"github.com/mbow/go-xsearch/bloom"' --include='*.go' | grep -v '_test.go' | grep -v 'bloom/'`
Run: `cd /home/mbow/code/search && grep -r '"github.com/mbow/go-xsearch/index"' --include='*.go' | grep -v '_test.go' | grep -v 'index/'`
Run: `cd /home/mbow/code/search && grep -r '"github.com/mbow/go-xsearch/bm25"' --include='*.go' | grep -v '_test.go' | grep -v 'bm25/'`
Run: `cd /home/mbow/code/search && grep -r '"github.com/mbow/go-xsearch/engine"' --include='*.go' | grep -v '_test.go' | grep -v 'engine/'`

Expected: No results for any of these.

- [ ] **Step 2: Delete old packages**

```bash
rm -rf bloom/ index/ bm25/ engine/
```

- [ ] **Step 3: Run full test suite**

Run: `cd /home/mbow/code/search && go test ./...`
Expected: All PASS. No compilation errors.

- [ ] **Step 4: Run benchmarks to verify no regression**

Run: `cd /home/mbow/code/search && go test ./benchmarks/ -bench=. -benchmem -count=5 | tee bench-latest.txt`

Compare with `bench-prev.txt` to check for regressions.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: delete old bloom, index, bm25, engine packages

These are now part of the xsearch library package."
```

---

### Task 17: Final Integration Verification

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `cd /home/mbow/code/search && go test ./... -v -count=1`
Expected: All PASS across all packages.

- [ ] **Step 2: Run go vet**

Run: `cd /home/mbow/code/search && go vet ./...`
Expected: No issues.

- [ ] **Step 3: Verify the app builds and starts**

Run: `cd /home/mbow/code/search && go build -o /tmp/xsearch . && /tmp/xsearch &`
Expected: Server starts on `127.0.0.1:8080`.

Run: `curl -s 'http://127.0.0.1:8080/search?q=bud' | head -5`
Expected: HTML fragment with search results.

Kill the server: `kill %1`

- [ ] **Step 4: Verify library is independently importable**

Run: `cd /tmp && mkdir xsearch-test && cd xsearch-test && go mod init test && go get github.com/mbow/go-xsearch/xsearch`

Or verify locally: the `xsearch/` package has no imports of other project packages.

Run: `cd /home/mbow/code/search && go list -f '{{.Imports}}' ./xsearch/ | tr ' ' '\n' | grep 'go-xsearch' | grep -v 'xsearch'`
Expected: No output (xsearch imports nothing from the rest of the project).

- [ ] **Step 5: Final commit if any cleanup needed**

```bash
git status
# If clean, no commit needed
```
