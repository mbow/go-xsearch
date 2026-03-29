# Autocomplete Search Engine — Implementation Plan A

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go stdlib-only autocomplete backend with Bloom filter, n-gram inverted index, popularity ranking with recency decay, category fallback, and an HTMX frontend.

**Architecture:** Layered system — HTTP serves HTMX fragments, search engine orchestrates Bloom filter (fast rejection), n-gram index (fuzzy/prefix/substring matching via trigram Jaccard similarity), ranking engine (exponential decay popularity + relevance fusion), and category fallback. All in-memory, popularity snapshots to JSON on disk.

**Tech Stack:** Go 1.26 stdlib only, HTMX (vendored JS), `html/template`, `net/http`, `encoding/json`

---

## File Map

| File | Responsibility |
|------|---------------|
| `catalog/catalog.go` | Product struct, load products from JSON |
| `catalog/catalog_test.go` | Tests for catalog loading |
| `bloom/bloom.go` | Bloom filter: bit array, FNV-1a/DJB2 hashing, Add, MayContain |
| `bloom/bloom_test.go` | Tests for Bloom filter |
| `index/ngram.go` | Trigram extraction, inverted index, Jaccard scoring, short query prefix fallback |
| `index/ngram_test.go` | Tests for n-gram index |
| `ranking/ranking.go` | Popularity tracking, exponential decay, score fusion, persistence |
| `ranking/ranking_test.go` | Tests for ranking engine |
| `engine/engine.go` | Search orchestrator: bloom -> index -> category fallback -> ranking |
| `engine/engine_test.go` | Integration tests for full search flow |
| `main.go` | HTTP server, routes, template rendering, startup |
| `main_test.go` | HTTP handler tests |
| `templates/index.html` | Full page with HTMX search input |
| `templates/results.html` | HTML fragment for search results |
| `static/htmx.min.js` | Vendored HTMX library |
| `data/products.json` | Sample product catalog |

---

### Task 1: Catalog Package — Product Loading

**Files:**
- Create: `catalog/catalog.go`
- Create: `catalog/catalog_test.go`
- Create: `data/products.json`

- [ ] **Step 1: Create sample product data**

Create `data/products.json`:

```json
[
  {"name": "Budweiser", "category": "beer"},
  {"name": "Miller Lite", "category": "beer"},
  {"name": "Coors Light", "category": "beer"},
  {"name": "Heineken", "category": "beer"},
  {"name": "Corona Extra", "category": "beer"},
  {"name": "Nike Air Max", "category": "shoes"},
  {"name": "Nike Dunk Low", "category": "shoes"},
  {"name": "Adidas Superstar", "category": "shoes"},
  {"name": "New Balance 574", "category": "shoes"},
  {"name": "Converse Chuck Taylor", "category": "shoes"},
  {"name": "Samsung Galaxy S24", "category": "phone"},
  {"name": "iPhone 16 Pro", "category": "phone"},
  {"name": "Google Pixel 9", "category": "phone"},
  {"name": "Sony WH-1000XM5", "category": "headphones"},
  {"name": "AirPods Pro", "category": "headphones"},
  {"name": "Bose QuietComfort", "category": "headphones"},
  {"name": "Coca-Cola", "category": "soda"},
  {"name": "Pepsi", "category": "soda"},
  {"name": "Sprite", "category": "soda"},
  {"name": "Mountain Dew", "category": "soda"}
]
```

- [ ] **Step 2: Write failing tests for catalog loading**

Create `catalog/catalog_test.go`:

```go
package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProducts(t *testing.T) {
	// Create a temp JSON file
	dir := t.TempDir()
	path := filepath.Join(dir, "products.json")
	data := `[
		{"name": "Budweiser", "category": "beer"},
		{"name": "Nike Air Max", "category": "shoes"}
	]`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	products, err := LoadProducts(path)
	if err != nil {
		t.Fatalf("LoadProducts() error: %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}

	if products[0].Name != "Budweiser" {
		t.Errorf("expected name 'Budweiser', got %q", products[0].Name)
	}
	if products[0].Category != "beer" {
		t.Errorf("expected category 'beer', got %q", products[0].Category)
	}
	if products[1].Name != "Nike Air Max" {
		t.Errorf("expected name 'Nike Air Max', got %q", products[1].Name)
	}
	if products[1].Category != "shoes" {
		t.Errorf("expected category 'shoes', got %q", products[1].Category)
	}
}

func TestLoadProductsFileNotFound(t *testing.T) {
	_, err := LoadProducts("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoadProductsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadProducts(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./catalog/ -v`
Expected: FAIL — `LoadProducts` and `Product` not defined.

- [ ] **Step 4: Implement catalog package**

Create `catalog/catalog.go`:

```go
package catalog

import (
	"encoding/json"
	"os"
)

// Product represents a single item in the product catalog.
type Product struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// LoadProducts reads a JSON file and returns a slice of products.
func LoadProducts(path string) ([]Product, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var products []Product
	if err := json.Unmarshal(data, &products); err != nil {
		return nil, err
	}

	return products, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./catalog/ -v`
Expected: PASS — all 3 tests pass.

- [ ] **Step 6: Commit**

```bash
git add catalog/ data/products.json
git commit -m "feat(catalog): add product loading from JSON"
```

---

### Task 2: Bloom Filter — Hash Functions

**Files:**
- Create: `bloom/bloom.go`
- Create: `bloom/bloom_test.go`

- [ ] **Step 1: Write failing tests for hash functions**

Create `bloom/bloom_test.go`:

```go
package bloom

import "testing"

func TestFnv1a(t *testing.T) {
	h1 := fnv1a("hello")
	h2 := fnv1a("hello")
	h3 := fnv1a("world")

	if h1 != h2 {
		t.Errorf("same input should produce same hash: %d != %d", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different inputs should (usually) produce different hashes")
	}
	if h1 == 0 {
		t.Errorf("hash should not be zero for non-empty input")
	}
}

func TestDjb2(t *testing.T) {
	h1 := djb2("hello")
	h2 := djb2("hello")
	h3 := djb2("world")

	if h1 != h2 {
		t.Errorf("same input should produce same hash: %d != %d", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different inputs should (usually) produce different hashes")
	}
	if h1 == 0 {
		t.Errorf("hash should not be zero for non-empty input")
	}
}

func TestHashIndependence(t *testing.T) {
	f := fnv1a("test")
	d := djb2("test")
	if f == d {
		t.Errorf("fnv1a and djb2 should produce different hashes for same input: both %d", f)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./bloom/ -v`
Expected: FAIL — `fnv1a` and `djb2` not defined.

- [ ] **Step 3: Implement hash functions**

Create `bloom/bloom.go`:

```go
package bloom

// fnv1a computes FNV-1a hash for the given string.
func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// djb2 computes DJB2 hash for the given string.
func djb2(s string) uint64 {
	h := uint64(5381)
	for i := 0; i < len(s); i++ {
		h = ((h << 5) + h) + uint64(s[i])
	}
	return h
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./bloom/ -v`
Expected: PASS — all 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add bloom/
git commit -m "feat(bloom): add FNV-1a and DJB2 hash functions"
```

---

### Task 3: Bloom Filter — Core Data Structure

**Files:**
- Modify: `bloom/bloom.go`
- Modify: `bloom/bloom_test.go`

- [ ] **Step 1: Write failing tests for Bloom filter**

Append to `bloom/bloom_test.go`:

```go
func TestNewFilter(t *testing.T) {
	f := New(1000, 3)
	if f == nil {
		t.Fatal("New() returned nil")
	}
}

func TestAddAndMayContain(t *testing.T) {
	f := New(1000, 3)
	f.Add("sho")
	f.Add("hoe")
	f.Add("oes")

	if !f.MayContain("sho") {
		t.Error("MayContain('sho') should return true after Add")
	}
	if !f.MayContain("hoe") {
		t.Error("MayContain('hoe') should return true after Add")
	}
	if !f.MayContain("oes") {
		t.Error("MayContain('oes') should return true after Add")
	}
}

func TestMayContainNeverAdded(t *testing.T) {
	f := New(20000, 3)

	f.Add("abc")
	f.Add("def")
	f.Add("ghi")

	// These were never added — with a large bit array, false positives are rare.
	// We test several to make sure at least most return false.
	notAdded := []string{"zzz", "qqq", "xxx", "yyy", "www", "vvv", "uuu", "ppp"}
	falsePositives := 0
	for _, s := range notAdded {
		if f.MayContain(s) {
			falsePositives++
		}
	}
	if falsePositives > 2 {
		t.Errorf("too many false positives: %d out of %d", falsePositives, len(notAdded))
	}
}

func TestNoFalseNegatives(t *testing.T) {
	f := New(20000, 3)
	items := []string{"abc", "def", "ghi", "jkl", "mno", "pqr", "stu", "vwx"}

	for _, item := range items {
		f.Add(item)
	}

	for _, item := range items {
		if !f.MayContain(item) {
			t.Errorf("false negative for %q — this must never happen", item)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./bloom/ -v -run "TestNew|TestAdd|TestMayContain|TestNoFalse"`
Expected: FAIL — `New`, `Filter`, `Add`, `MayContain` not defined.

- [ ] **Step 3: Implement Bloom filter struct**

Add to `bloom/bloom.go` (after the hash functions):

```go
// Filter is a Bloom filter backed by a bit array.
type Filter struct {
	bits []uint64
	size uint64
	k    int
}

// New creates a Bloom filter with the given number of bits and hash count.
func New(numBits uint64, k int) *Filter {
	// Round up to multiple of 64
	words := (numBits + 63) / 64
	return &Filter{
		bits: make([]uint64, words),
		size: words * 64,
		k:    k,
	}
}

// hashes returns k bit positions for the given item using double hashing.
func (f *Filter) hashes(item string) []uint64 {
	h1 := fnv1a(item)
	h2 := djb2(item)
	positions := make([]uint64, f.k)
	for i := 0; i < f.k; i++ {
		positions[i] = (h1 + uint64(i)*h2) % f.size
	}
	return positions
}

// Add inserts an item into the Bloom filter.
func (f *Filter) Add(item string) {
	for _, pos := range f.hashes(item) {
		word := pos / 64
		bit := pos % 64
		f.bits[word] |= 1 << bit
	}
}

// MayContain checks if an item might be in the filter.
// Returns false if the item is definitely not in the set.
// Returns true if the item might be in the set (possible false positive).
func (f *Filter) MayContain(item string) bool {
	for _, pos := range f.hashes(item) {
		word := pos / 64
		bit := pos % 64
		if f.bits[word]&(1<<bit) == 0 {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./bloom/ -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
git add bloom/
git commit -m "feat(bloom): add Bloom filter with double hashing"
```

---

### Task 4: N-gram Index — Trigram Extraction

**Files:**
- Create: `index/ngram.go`
- Create: `index/ngram_test.go`

- [ ] **Step 1: Write failing tests for trigram extraction**

Create `index/ngram_test.go`:

```go
package index

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractTrigrams(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"shoes", []string{"sho", "hoe", "oes"}},
		{"hi", nil},
		{"a", nil},
		{"", nil},
		{"abc", []string{"abc"}},
		{"SHOES", []string{"sho", "hoe", "oes"}},
		{"  Nike  ", []string{"nik", "ike"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractTrigrams(tt.input)
			sort.Strings(got)
			expected := tt.expected
			sort.Strings(expected)
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("ExtractTrigrams(%q) = %v, want %v", tt.input, got, expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./index/ -v`
Expected: FAIL — `ExtractTrigrams` not defined.

- [ ] **Step 3: Implement trigram extraction**

Create `index/ngram.go`:

```go
package index

import "strings"

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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./index/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add index/
git commit -m "feat(index): add trigram extraction with normalization"
```

---

### Task 5: N-gram Index — Building and Querying the Inverted Index

**Files:**
- Modify: `index/ngram.go`
- Modify: `index/ngram_test.go`

- [ ] **Step 1: Write failing tests for index building and search**

Append to `index/ngram_test.go`:

```go
import (
	"search/catalog"
)
```

Update the import block to include `search/catalog`, then append:

```go
func testProducts() []catalog.Product {
	return []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
		{Name: "Nike Dunk Low", Category: "shoes"},
		{Name: "AirPods Pro", Category: "headphones"},
	}
}

func TestNewIndex(t *testing.T) {
	idx := NewIndex(testProducts())
	if idx == nil {
		t.Fatal("NewIndex returned nil")
	}
}

func TestSearchPrefix(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("nik")
	if len(results) == 0 {
		t.Fatal("expected results for 'nik'")
	}

	foundNikeAir := false
	foundNikeDunk := false
	for _, r := range results {
		if r.ProductID == 2 {
			foundNikeAir = true
		}
		if r.ProductID == 3 {
			foundNikeDunk = true
		}
	}
	if !foundNikeAir {
		t.Error("expected to find Nike Air Max (ID 2)")
	}
	if !foundNikeDunk {
		t.Error("expected to find Nike Dunk Low (ID 3)")
	}
}

func TestSearchSubstring(t *testing.T) {
	idx := NewIndex(testProducts())
	// "pod" is a substring of "AirPods Pro"
	results := idx.Search("pod")
	found := false
	for _, r := range results {
		if r.ProductID == 4 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find AirPods Pro (ID 4) searching for 'pod'")
	}
}

func TestSearchFuzzy(t *testing.T) {
	idx := NewIndex(testProducts())
	// "budwiser" is a typo of "Budweiser" — shares trigrams "bud", "udw", "wis", "ise"
	results := idx.Search("budwiser")
	found := false
	for _, r := range results {
		if r.ProductID == 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Budweiser (ID 0) with fuzzy search 'budwiser'")
	}
}

func TestSearchShortQuery(t *testing.T) {
	idx := NewIndex(testProducts())
	// 1-2 char queries use prefix fallback
	results := idx.Search("ni")
	found := false
	for _, r := range results {
		if r.ProductID == 2 || r.ProductID == 3 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Nike products with short query 'ni'")
	}
}

func TestSearchEmpty(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("")
	if len(results) != 0 {
		t.Errorf("expected no results for empty query, got %d", len(results))
	}
}

func TestSearchResultScore(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("budweiser")
	for _, r := range results {
		if r.ProductID == 0 {
			if r.Score < 0.5 {
				t.Errorf("exact match for Budweiser should have high score, got %f", r.Score)
			}
			return
		}
	}
	t.Error("expected to find Budweiser in results")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./index/ -v -run "TestNew|TestSearch"`
Expected: FAIL — `NewIndex`, `Search`, `SearchResult` not defined.

- [ ] **Step 3: Implement inverted index**

Add to `index/ngram.go`:

```go
import (
	"search/catalog"
	"strings"
)

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
```

Note: replace the existing import of just `"strings"` with the new import block that includes `"search/catalog"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./index/ -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
git add index/
git commit -m "feat(index): add n-gram inverted index with Jaccard scoring"
```

---

### Task 6: N-gram Index — Category Search

**Files:**
- Modify: `index/ngram.go`
- Modify: `index/ngram_test.go`

- [ ] **Step 1: Write failing tests for category search**

Append to `index/ngram_test.go`:

```go
func TestSearchCategories(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.SearchCategories("bee")
	if len(results) == 0 {
		t.Fatal("expected category results for 'bee'")
	}

	if results[0] != "beer" {
		t.Errorf("expected best category 'beer', got %q", results[0])
	}
}

func TestSearchCategoriesNoMatch(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.SearchCategories("zzz")
	if len(results) != 0 {
		t.Errorf("expected no category results for 'zzz', got %v", results)
	}
}

func TestProductsByCategory(t *testing.T) {
	idx := NewIndex(testProducts())
	ids := idx.ProductsByCategory("beer")
	if len(ids) != 2 {
		t.Errorf("expected 2 beer products, got %d", len(ids))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./index/ -v -run "TestSearchCategories|TestProductsByCategory"`
Expected: FAIL — `SearchCategories`, `ProductsByCategory` not defined.

- [ ] **Step 3: Implement category search**

Add to `index/ngram.go`, in the `Index` struct add a new field:

```go
type Index struct {
	products      []catalog.Product
	posting       map[string][]int
	trigrams      map[int]map[string]struct{}
	catTrigrams   map[string]map[string]struct{} // category name -> set of its trigrams
	catProducts   map[string][]int               // category name -> list of product IDs
}
```

Update `NewIndex` to build category data:

```go
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
```

Add the new methods:

```go
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
```

Update the imports in `ngram.go` to:

```go
import (
	"search/catalog"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./index/ -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
git add index/
git commit -m "feat(index): add category trigram search and product-by-category lookup"
```

---

### Task 7: Ranking Engine — Exponential Decay Popularity

**Files:**
- Create: `ranking/ranking.go`
- Create: `ranking/ranking_test.go`

- [ ] **Step 1: Write failing tests for popularity scoring**

Create `ranking/ranking_test.go`:

```go
package ranking

import (
	"math"
	"testing"
	"time"
)

func TestRecordSelection(t *testing.T) {
	r := New(0.05, 0.6)
	r.RecordSelection(1)
	r.RecordSelection(1)
	r.RecordSelection(2)

	score1 := r.PopularityScore(1)
	score2 := r.PopularityScore(2)

	if score1 <= score2 {
		t.Errorf("product 1 (2 selections) should score higher than product 2 (1 selection): %f <= %f", score1, score2)
	}
}

func TestPopularityScoreDecay(t *testing.T) {
	r := New(0.05, 0.6)

	now := time.Now()
	// Inject timestamps directly for testing
	r.SetSelections(1, []time.Time{now})
	r.SetSelections(2, []time.Time{now.Add(-14 * 24 * time.Hour)}) // 14 days ago

	score1 := r.PopularityScore(1)
	score2 := r.PopularityScore(2)

	if score1 <= score2 {
		t.Errorf("recent selection should score higher than 14-day-old: %f <= %f", score1, score2)
	}

	// 14 days at lambda=0.05: e^(-0.05*14) ≈ 0.497
	expectedDecay := math.Exp(-0.05 * 14)
	if math.Abs(score2-expectedDecay) > 0.01 {
		t.Errorf("expected score2 ≈ %f, got %f", expectedDecay, score2)
	}
}

func TestPopularityScoreNoSelections(t *testing.T) {
	r := New(0.05, 0.6)
	score := r.PopularityScore(99)
	if score != 0 {
		t.Errorf("expected 0 for unselected product, got %f", score)
	}
}

func TestNormalizedPopularity(t *testing.T) {
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{now, now, now})
	r.SetSelections(2, []time.Time{now})

	norm1 := r.NormalizedPopularity(1)
	norm2 := r.NormalizedPopularity(2)

	if norm1 != 1.0 {
		t.Errorf("max popularity product should normalize to 1.0, got %f", norm1)
	}
	if norm2 <= 0 || norm2 >= 1.0 {
		t.Errorf("lower popularity should normalize between 0 and 1, got %f", norm2)
	}
}

func TestNormalizedPopularityNoSelections(t *testing.T) {
	r := New(0.05, 0.6)
	norm := r.NormalizedPopularity(1)
	if norm != 0 {
		t.Errorf("expected 0 when no selections exist, got %f", norm)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v`
Expected: FAIL — `New`, `Ranker`, etc. not defined.

- [ ] **Step 3: Implement ranking engine**

Create `ranking/ranking.go`:

```go
package ranking

import (
	"math"
	"sync"
	"time"
)

// Ranker tracks product selection popularity with exponential time decay.
type Ranker struct {
	mu         sync.RWMutex
	lambda     float64                // decay rate
	alpha      float64                // relevance vs popularity weight
	selections map[int][]time.Time    // product ID -> selection timestamps
}

// New creates a Ranker with the given decay rate and alpha.
func New(lambda, alpha float64) *Ranker {
	return &Ranker{
		lambda:     lambda,
		alpha:      alpha,
		selections: make(map[int][]time.Time),
	}
}

// RecordSelection records a user selecting a product right now.
func (r *Ranker) RecordSelection(productID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = append(r.selections[productID], time.Now())
}

// SetSelections sets the selection timestamps for a product directly (used for testing and loading from disk).
func (r *Ranker) SetSelections(productID int, timestamps []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = timestamps
}

// PopularityScore computes the raw exponential decay score for a product.
func (r *Ranker) PopularityScore(productID int) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	timestamps := r.selections[productID]
	if len(timestamps) == 0 {
		return 0
	}

	now := time.Now()
	var score float64
	for _, ts := range timestamps {
		ageDays := now.Sub(ts).Hours() / 24
		score += math.Exp(-r.lambda * ageDays)
	}
	return score
}

// NormalizedPopularity returns the popularity score normalized to 0.0-1.0
// by dividing by the maximum popularity score across all products.
func (r *Ranker) NormalizedPopularity(productID int) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	maxScore := 0.0
	now := time.Now()
	for _, timestamps := range r.selections {
		var s float64
		for _, ts := range timestamps {
			ageDays := now.Sub(ts).Hours() / 24
			s += math.Exp(-r.lambda * ageDays)
		}
		if s > maxScore {
			maxScore = s
		}
	}

	if maxScore == 0 {
		return 0
	}

	var score float64
	for _, ts := range r.selections[productID] {
		ageDays := now.Sub(ts).Hours() / 24
		score += math.Exp(-r.lambda * ageDays)
	}
	return score / maxScore
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v`
Expected: PASS — all 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add ranking/
git commit -m "feat(ranking): add exponential decay popularity scoring with normalization"
```

---

### Task 8: Ranking Engine — Score Fusion

**Files:**
- Modify: `ranking/ranking.go`
- Modify: `ranking/ranking_test.go`

- [ ] **Step 1: Write failing tests for score fusion**

Append to `ranking/ranking_test.go`:

```go
func TestCombinedScore(t *testing.T) {
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{now, now, now})
	r.SetSelections(2, []time.Time{now})

	// Product 1: high popularity, low relevance
	score1 := r.CombinedScore(1, 0.3)
	// Product 2: low popularity, high relevance
	score2 := r.CombinedScore(2, 0.9)

	// With alpha=0.6, relevance dominates, so product 2 should win
	if score2 <= score1 {
		t.Errorf("high relevance should beat high popularity at alpha=0.6: score1=%f, score2=%f", score1, score2)
	}
}

func TestCombinedScoreZeroPopularity(t *testing.T) {
	r := New(0.05, 0.6)
	score := r.CombinedScore(99, 0.8)
	// With no popularity data, score = alpha * relevance = 0.6 * 0.8 = 0.48
	expected := 0.6 * 0.8
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected %f, got %f", expected, score)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v -run "TestCombined"`
Expected: FAIL — `CombinedScore` not defined.

- [ ] **Step 3: Implement score fusion**

Add to `ranking/ranking.go`:

```go
// CombinedScore computes the final ranking score by fusing relevance and popularity.
// relevance should be in the 0.0-1.0 range (Jaccard similarity).
func (r *Ranker) CombinedScore(productID int, relevance float64) float64 {
	pop := r.NormalizedPopularity(productID)
	return r.alpha*relevance + (1-r.alpha)*pop
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
git add ranking/
git commit -m "feat(ranking): add combined score fusion (relevance + popularity)"
```

---

### Task 9: Ranking Engine — Persistence (Save/Load)

**Files:**
- Modify: `ranking/ranking.go`
- Modify: `ranking/ranking_test.go`

- [ ] **Step 1: Write failing tests for persistence**

Append to `ranking/ranking_test.go`:

```go
import (
	"os"
	"path/filepath"
)
```

Update the import block to include `"os"` and `"path/filepath"`, then append:

```go
func TestSaveAndLoad(t *testing.T) {
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{now, now.Add(-24 * time.Hour)})
	r.SetSelections(5, []time.Time{now.Add(-48 * time.Hour)})

	dir := t.TempDir()
	path := filepath.Join(dir, "popularity.json")

	if err := r.Save(path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	r2 := New(0.05, 0.6)
	if err := r2.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Verify loaded data matches
	r.mu.RLock()
	r2.mu.RLock()
	defer r.mu.RUnlock()
	defer r2.mu.RUnlock()

	if len(r2.selections[1]) != 2 {
		t.Errorf("expected 2 selections for product 1, got %d", len(r2.selections[1]))
	}
	if len(r2.selections[5]) != 1 {
		t.Errorf("expected 1 selection for product 5, got %d", len(r2.selections[5]))
	}
}

func TestLoadNonexistent(t *testing.T) {
	r := New(0.05, 0.6)
	err := r.Load("/nonexistent/path.json")
	// Should not error — missing file means no prior data
	if err != nil {
		t.Errorf("Load() should not error for missing file, got: %v", err)
	}
}

func TestPrune(t *testing.T) {
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{
		now,
		now.Add(-91 * 24 * time.Hour), // older than 90 days
	})

	r.Prune(90)

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.selections[1]) != 1 {
		t.Errorf("expected 1 selection after prune, got %d", len(r.selections[1]))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v -run "TestSave|TestLoad|TestPrune"`
Expected: FAIL — `Save`, `Load`, `Prune` not defined.

- [ ] **Step 3: Implement persistence and pruning**

Add to `ranking/ranking.go`:

```go
import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"sync"
	"time"
)
```

Replace the existing imports with the above, then add:

```go
// Save writes all selection data to a JSON file.
func (r *Ranker) Save(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := json.Marshal(r.selections)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads selection data from a JSON file. If the file does not exist,
// this is a no-op (fresh start with no prior popularity data).
func (r *Ranker) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return json.Unmarshal(data, &r.selections)
}

// Prune removes selection timestamps older than maxAgeDays.
func (r *Ranker) Prune(maxAgeDays int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	for id, timestamps := range r.selections {
		kept := timestamps[:0]
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		if len(kept) == 0 {
			delete(r.selections, id)
		} else {
			r.selections[id] = kept
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./ranking/ -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
git add ranking/
git commit -m "feat(ranking): add save/load persistence and timestamp pruning"
```

---

### Task 10: Search Engine — Orchestrator

**Files:**
- Create: `engine/engine.go`
- Create: `engine/engine_test.go`

- [ ] **Step 1: Write failing tests for search engine**

Create `engine/engine_test.go`:

```go
package engine

import (
	"search/catalog"
	"testing"
)

func testProducts() []catalog.Product {
	return []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Coors Light", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
		{Name: "Nike Dunk Low", Category: "shoes"},
		{Name: "AirPods Pro", Category: "headphones"},
	}
}

func TestNewEngine(t *testing.T) {
	e := New(testProducts())
	if e == nil {
		t.Fatal("New() returned nil")
	}
}

func TestSearchDirectMatch(t *testing.T) {
	e := New(testProducts())
	results := e.Search("nike")
	if len(results) == 0 {
		t.Fatal("expected results for 'nike'")
	}

	foundDirect := false
	for _, r := range results {
		if r.MatchType == MatchDirect {
			foundDirect = true
		}
	}
	if !foundDirect {
		t.Error("expected direct match results for 'nike'")
	}
}

func TestSearchBloomRejection(t *testing.T) {
	e := New(testProducts())
	// Completely unrelated query — Bloom should reject most/all trigrams
	results := e.Search("xzqwvp")
	// May still get category fallback results, but no direct matches
	for _, r := range results {
		if r.MatchType == MatchDirect {
			t.Error("expected no direct matches for gibberish query")
		}
	}
}

func TestSearchCategoryFallback(t *testing.T) {
	e := New(testProducts())
	// "budwiser" is a typo but close enough to find "Budweiser"
	// However "heineken" (not in catalog) searching "lager" should trigger fallback
	// Let's test with a query that matches a category but no product directly
	results := e.Search("beer")
	if len(results) == 0 {
		t.Fatal("expected results for 'beer'")
	}

	// "beer" matches the category name — should find beer products
	foundBeer := false
	for _, r := range results {
		if r.Product.Category == "beer" {
			foundBeer = true
		}
	}
	if !foundBeer {
		t.Error("expected beer products in results")
	}
}

func TestSearchFuzzyMatch(t *testing.T) {
	e := New(testProducts())
	results := e.Search("budwiser")
	foundBud := false
	for _, r := range results {
		if r.Product.Name == "Budweiser" {
			foundBud = true
		}
	}
	if !foundBud {
		t.Error("expected to find Budweiser via fuzzy match for 'budwiser'")
	}
}

func TestSearchSortedByScore(t *testing.T) {
	e := New(testProducts())
	results := e.Search("nike")
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: score[%d]=%f > score[%d]=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestSearchRecordAndRank(t *testing.T) {
	e := New(testProducts())

	// Select Nike Dunk Low several times
	for i := 0; i < 10; i++ {
		e.RecordSelection(4) // Nike Dunk Low
	}

	results := e.Search("nike")
	if len(results) < 2 {
		t.Fatal("expected at least 2 results for 'nike'")
	}

	// Nike Dunk Low should rank higher than Nike Air Max due to popularity
	dunkIdx := -1
	airIdx := -1
	for i, r := range results {
		if r.Product.Name == "Nike Dunk Low" {
			dunkIdx = i
		}
		if r.Product.Name == "Nike Air Max" {
			airIdx = i
		}
	}

	if dunkIdx == -1 || airIdx == -1 {
		t.Fatal("expected both Nike products in results")
	}
	if dunkIdx > airIdx {
		t.Error("Nike Dunk Low should rank higher than Nike Air Max due to popularity")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test ./engine/ -v`
Expected: FAIL — `New`, `Engine`, `Search`, `MatchDirect`, `Result` not defined.

- [ ] **Step 3: Implement search engine**

Create `engine/engine.go`:

```go
package engine

import (
	"search/bloom"
	"search/catalog"
	"search/index"
	"search/ranking"
	"sort"
)

// MatchType indicates how a result was found.
type MatchType int

const (
	MatchDirect   MatchType = iota // Found via direct n-gram match
	MatchFallback                  // Found via category fallback
)

// Result is a single search result with metadata.
type Result struct {
	Product   catalog.Product
	ProductID int
	Score     float64
	MatchType MatchType
}

// Engine orchestrates search across all components.
type Engine struct {
	products []catalog.Product
	bloom    *bloom.Filter
	index    *index.Index
	ranker   *ranking.Ranker
}

const (
	bloomSize     = 20000
	bloomHashCount = 3
	lambda        = 0.05
	alpha         = 0.6
	fallbackThreshold = 0.2
	minDirectResults  = 3
	fallbackRelevance = 0.1
	maxResults        = 10
)

// New creates a search engine from the given product catalog.
func New(products []catalog.Product) *Engine {
	e := &Engine{
		products: products,
		bloom:    bloom.New(bloomSize, bloomHashCount),
		index:    index.NewIndex(products),
		ranker:   ranking.New(lambda, alpha),
	}

	// Populate Bloom filter with all product and category trigrams
	for _, p := range products {
		for _, g := range index.ExtractTrigrams(p.Name) {
			e.bloom.Add(g)
		}
		for _, g := range index.ExtractTrigrams(p.Category) {
			e.bloom.Add(g)
		}
	}

	return e
}

// Search returns ranked results for the given query.
func (e *Engine) Search(query string) []Result {
	if query == "" {
		return nil
	}

	trigrams := index.ExtractTrigrams(query)

	// Short query bypass — skip Bloom, go straight to prefix search
	if len(trigrams) == 0 {
		return e.buildResults(e.index.Search(query), MatchDirect)
	}

	// Bloom filter check — fast rejection
	anyPass := false
	for _, g := range trigrams {
		if e.bloom.MayContain(g) {
			anyPass = true
			break
		}
	}

	var results []Result

	if anyPass {
		searchResults := e.index.Search(query)

		// Count how many pass the quality threshold
		goodResults := 0
		for _, sr := range searchResults {
			if sr.Score >= fallbackThreshold {
				goodResults++
			}
		}

		results = e.buildResults(searchResults, MatchDirect)

		// If not enough good direct results, add category fallback
		if goodResults < minDirectResults {
			fallbackResults := e.categoryFallback(query, searchResults)
			results = append(results, fallbackResults...)
		}
	} else {
		// Bloom rejected everything — try category fallback only
		results = e.categoryFallback(query, nil)
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Limit results
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// RecordSelection records a user selecting a product.
func (e *Engine) RecordSelection(productID int) {
	e.ranker.RecordSelection(productID)
}

// Ranker returns the underlying ranker for persistence operations.
func (e *Engine) Ranker() *ranking.Ranker {
	return e.ranker
}

// buildResults converts index search results to engine results with combined scores.
func (e *Engine) buildResults(searchResults []index.SearchResult, matchType MatchType) []Result {
	results := make([]Result, 0, len(searchResults))
	for _, sr := range searchResults {
		if sr.ProductID < 0 || sr.ProductID >= len(e.products) {
			continue
		}
		score := e.ranker.CombinedScore(sr.ProductID, sr.Score)
		results = append(results, Result{
			Product:   e.products[sr.ProductID],
			ProductID: sr.ProductID,
			Score:     score,
			MatchType: matchType,
		})
	}
	return results
}

// categoryFallback finds the best matching category and returns its popular products.
func (e *Engine) categoryFallback(query string, exclude []index.SearchResult) []Result {
	categories := e.index.SearchCategories(query)
	if len(categories) == 0 {
		return nil
	}

	// Build set of already-found product IDs to avoid duplicates
	seen := make(map[int]struct{})
	for _, sr := range exclude {
		seen[sr.ProductID] = struct{}{}
	}

	var results []Result
	// Use the best matching category
	productIDs := e.index.ProductsByCategory(categories[0])
	for _, id := range productIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		if id < 0 || id >= len(e.products) {
			continue
		}
		score := e.ranker.CombinedScore(id, fallbackRelevance)
		results = append(results, Result{
			Product:   e.products[id],
			ProductID: id,
			Score:     score,
			MatchType: MatchFallback,
		})
	}

	return results
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test ./engine/ -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
git add engine/
git commit -m "feat(engine): add search orchestrator with Bloom, n-gram, category fallback, and ranking"
```

---

### Task 11: HTMX Frontend — Templates and Static Files

**Files:**
- Create: `templates/index.html`
- Create: `templates/results.html`
- Create: `static/htmx.min.js` (placeholder — will vendor actual file)

- [ ] **Step 1: Create the main page template**

Create `templates/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Product Search</title>
    <script src="/static/htmx.min.js"></script>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: system-ui, sans-serif; max-width: 600px; margin: 40px auto; padding: 0 20px; }
        h1 { margin-bottom: 20px; }
        input[type="search"] {
            width: 100%; padding: 12px; font-size: 16px;
            border: 2px solid #ccc; border-radius: 8px;
        }
        input[type="search"]:focus { outline: none; border-color: #4a90d9; }
        #results { margin-top: 8px; }
        .result-item {
            padding: 10px 12px; border-bottom: 1px solid #eee; cursor: pointer;
        }
        .result-item:hover { background: #f5f5f5; }
        .result-name { font-weight: 500; }
        .result-category { color: #666; font-size: 14px; }
        .result-section { padding: 8px 12px; color: #888; font-size: 13px; font-weight: 600; background: #f9f9f9; }
        .htmx-indicator { display: none; }
        .htmx-request .htmx-indicator { display: inline; }
    </style>
</head>
<body>
    <h1>Product Search</h1>
    <div>
        <input type="search"
               name="q"
               placeholder="Search products..."
               autocomplete="off"
               hx-get="/search"
               hx-trigger="keyup changed delay:250ms, search"
               hx-target="#results"
               hx-indicator="#spinner">
        <span id="spinner" class="htmx-indicator"> Searching...</span>
    </div>
    <div id="results"></div>
</body>
</html>
```

- [ ] **Step 2: Create the results fragment template**

Create `templates/results.html`:

```html
{{if .DirectResults}}
<div class="result-section">Results</div>
{{range .DirectResults}}
<div class="result-item"
     hx-post="/select"
     hx-vals='{"id": "{{.ProductID}}"}'
     hx-swap="none">
    <div class="result-name">{{.Product.Name}}</div>
    <div class="result-category">{{.Product.Category}}</div>
</div>
{{end}}
{{end}}

{{if .FallbackResults}}
<div class="result-section">Related products</div>
{{range .FallbackResults}}
<div class="result-item"
     hx-post="/select"
     hx-vals='{"id": "{{.ProductID}}"}'
     hx-swap="none">
    <div class="result-name">{{.Product.Name}}</div>
    <div class="result-category">{{.Product.Category}}</div>
</div>
{{end}}
{{end}}

{{if and (not .DirectResults) (not .FallbackResults)}}
{{if .Query}}
<div class="result-section">No results found</div>
{{end}}
{{end}}
```

- [ ] **Step 3: Download and vendor HTMX**

Run: `curl -sL https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o /home/mbow/code/search/static/htmx.min.js`

Verify it downloaded: `head -c 100 /home/mbow/code/search/static/htmx.min.js`
Expected: JavaScript content starting with `(function(...)` or similar.

- [ ] **Step 4: Commit**

```bash
git add templates/ static/
git commit -m "feat: add HTMX frontend templates and vendored htmx.min.js"
```

---

### Task 12: HTTP Server — Handlers and Wiring

**Files:**
- Modify: `main.go`
- Create: `main_test.go`

- [ ] **Step 1: Write failing tests for HTTP handlers**

Create `main_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"search/catalog"
	"search/engine"
)

func testEngine() *engine.Engine {
	products := []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
	}
	return engine.New(products)
}

func TestHandleIndex(t *testing.T) {
	app := &App{engine: testEngine()}
	app.loadTemplates()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	app.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Product Search") {
		t.Error("expected page to contain 'Product Search'")
	}
	if !strings.Contains(body, "hx-get") {
		t.Error("expected page to contain HTMX attributes")
	}
}

func TestHandleSearch(t *testing.T) {
	app := &App{engine: testEngine()}
	app.loadTemplates()

	req := httptest.NewRequest("GET", "/search?q=nik", nil)
	w := httptest.NewRecorder()
	app.handleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Nike Air Max") {
		t.Error("expected results to contain 'Nike Air Max'")
	}
}

func TestHandleSearchEmpty(t *testing.T) {
	app := &App{engine: testEngine()}
	app.loadTemplates()

	req := httptest.NewRequest("GET", "/search?q=", nil)
	w := httptest.NewRecorder()
	app.handleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelect(t *testing.T) {
	app := &App{engine: testEngine()}

	body := strings.NewReader(`{"id": "0"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.handleSelect(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelectInvalidID(t *testing.T) {
	app := &App{engine: testEngine()}

	body := strings.NewReader(`{"id": "abc"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.handleSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/mbow/code/search && go test -v -run "TestHandle"`
Expected: FAIL — `App`, `handleIndex`, etc. not defined.

- [ ] **Step 3: Implement HTTP server**

Replace `main.go` entirely:

```go
package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"search/catalog"
	"search/engine"
)

// App holds application state.
type App struct {
	engine     *engine.Engine
	indexTmpl  *template.Template
	resultTmpl *template.Template
	dataDir    string
}

// ResultsData is the template data for search results.
type ResultsData struct {
	Query           string
	DirectResults   []engine.Result
	FallbackResults []engine.Result
}

func (app *App) loadTemplates() {
	app.indexTmpl = template.Must(template.ParseFiles("templates/index.html"))
	app.resultTmpl = template.Must(template.ParseFiles("templates/results.html"))
}

func (app *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	app.indexTmpl.Execute(w, nil)
}

func (app *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	results := app.engine.Search(query)

	data := ResultsData{Query: query}
	for _, res := range results {
		if res.MatchType == engine.MatchDirect {
			data.DirectResults = append(data.DirectResults, res)
		} else {
			data.FallbackResults = append(data.FallbackResults, res)
		}
	}

	app.resultTmpl.Execute(w, data)
}

func (app *App) handleSelect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(req.ID)
	if err != nil {
		http.Error(w, "invalid product ID", http.StatusBadRequest)
		return
	}

	app.engine.RecordSelection(id)
	w.WriteHeader(http.StatusOK)
}

func (app *App) startSnapshots(interval time.Duration) {
	path := filepath.Join(app.dataDir, "popularity.json")
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := app.engine.Ranker().Save(path); err != nil {
				log.Printf("error saving popularity data: %v", err)
			}
		}
	}()
}

func main() {
	dataDir := "data"

	// Load products
	products, err := catalog.LoadProducts(filepath.Join(dataDir, "products.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading products: %v\n", err)
		os.Exit(1)
	}

	app := &App{
		engine:  engine.New(products),
		dataDir: dataDir,
	}
	app.loadTemplates()

	// Load popularity data if it exists
	popPath := filepath.Join(dataDir, "popularity.json")
	if err := app.engine.Ranker().Load(popPath); err != nil {
		log.Printf("warning: could not load popularity data: %v", err)
	}

	// Prune old data and start periodic snapshots
	app.engine.Ranker().Prune(90)
	app.startSnapshots(60 * time.Second)

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("GET /search", app.handleSearch)
	mux.HandleFunc("POST /select", app.handleSelect)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	addr := ":8080"
	log.Printf("starting server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/mbow/code/search && go test -v -run "TestHandle"`
Expected: PASS — all 5 handler tests pass.

- [ ] **Step 5: Run all tests across all packages**

Run: `cd /home/mbow/code/search && go test ./... -v`
Expected: PASS — all tests across all packages pass.

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: add HTTP server with search, select, and template rendering"
```

---

### Task 13: End-to-End Smoke Test

**Files:**
- No new files — manual verification

- [ ] **Step 1: Build and verify compilation**

Run: `cd /home/mbow/code/search && go build -o search .`
Expected: builds without errors, produces `search` binary.

- [ ] **Step 2: Run all tests one final time**

Run: `cd /home/mbow/code/search && go test ./... -v -count=1`
Expected: ALL PASS.

- [ ] **Step 3: Run vet and check for issues**

Run: `cd /home/mbow/code/search && go vet ./...`
Expected: no issues reported.

- [ ] **Step 4: Clean up build artifact**

Run: `rm /home/mbow/code/search/search`

- [ ] **Step 5: Commit any final fixes if needed**

If any issues were found in steps 1-3, fix and commit them. Otherwise, skip this step.

---

## Summary

| Task | Component | What it builds |
|------|-----------|---------------|
| 1 | Catalog | Product struct, JSON loading |
| 2 | Bloom | FNV-1a and DJB2 hash functions |
| 3 | Bloom | Bit array, Add, MayContain |
| 4 | N-gram Index | Trigram extraction |
| 5 | N-gram Index | Inverted index, Jaccard scoring, prefix fallback |
| 6 | N-gram Index | Category trigram search |
| 7 | Ranking | Exponential decay popularity scoring |
| 8 | Ranking | Score fusion (relevance + popularity) |
| 9 | Ranking | Save/Load persistence, pruning |
| 10 | Engine | Search orchestrator wiring all components |
| 11 | Frontend | HTMX templates and static files |
| 12 | HTTP | Server handlers, routing, template rendering |
| 13 | Smoke Test | Build, vet, full test suite |
