# Performance at 100K + Typeahead UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Optimize search for 100K products (6 performance fixes) and add typeahead UX (match highlighting, ghost text, keyboard navigation) with no layout shift.

**Architecture:** Performance fixes target the BM25 and trigram hot paths — top-K heap select, pooled bitset dedup, prefix length cap, uint8 hit array, parallel prefix cache, binary search fallback. Typeahead UX adds highlight byte offsets to engine results, ghost text via data attribute, and ~40 lines of vanilla JS for keyboard nav.

**Tech Stack:** Go 1.26, container/heap, sync.Pool, HTMX 2.0.8, vanilla JS/CSS

**Spec:** `docs/superpowers/specs/2026-04-01-perf-typeahead-design.md`

---

### Task 0: Capture Baseline Benchmarks

**Files:**
- Create: `docs/benchmarks/baseline-pre-perf.txt`

- [ ] **Step 1: Run baseline benchmarks**

Run: `cd /home/mbow/code/search && go test -bench=. -benchmem -count=6 ./... 2>&1 | tee docs/benchmarks/baseline-pre-perf.txt`

Expected: All benchmarks pass and results are saved.

- [ ] **Step 2: Commit baseline**

```bash
git add docs/benchmarks/baseline-pre-perf.txt
git commit -m "bench: capture baseline before 100K performance + typeahead work"
```

---

### Task 1: Cap Prefix Length to 6 Characters

**Files:**
- Modify: `bm25/bm25.go:120-134`
- Modify: `bm25/bm25_test.go`

This is the simplest fix and reduces memory before we tackle the harder changes.

- [ ] **Step 1: Write failing test for prefix cap**

Append to `bm25/bm25_test.go`:

```go
func TestNewIndex_PrefixCap(t *testing.T) {
	products := []catalog.Product{
		{Name: "Weihenstephaner Hefeweissbier", Category: "beer"},
	}
	idx := NewIndex(products)

	// "weihen" (6 chars) should exist as a prefix
	if _, ok := idx.wordPrefixes[0]["weihen"]; !ok {
		t.Error("expected 'weihen' (6 chars) in word prefixes")
	}

	// "weihenst" (8 chars) should NOT exist — capped at 6
	if _, ok := idx.wordPrefixes[0]["weihenst"]; ok {
		t.Error("did NOT expect 'weihenst' (8 chars) — prefix should be capped at 6")
	}

	// Full word "weihenstephaner" should NOT be in prefixes
	if _, ok := idx.wordPrefixes[0]["weihenstephaner"]; ok {
		t.Error("did NOT expect full word in prefixes — capped at 6")
	}

	// Prefix posting should also be capped
	if _, ok := idx.prefixPosting["weihenst"]; ok {
		t.Error("did NOT expect 'weihenst' in prefix posting — capped at 6")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/mbow/code/search && go test ./bm25/ -run TestNewIndex_PrefixCap -v`

Expected: FAIL — "weihenst" IS found because prefix is currently uncapped.

- [ ] **Step 3: Cap prefix generation at 6**

Edit `bm25/bm25.go` line 125. Change:

```go
			for length := 1; length <= len(runes); length++ {
```

to:

```go
			maxPfx := min(len(runes), 6)
			for length := 1; length <= maxPfx; length++ {
```

- [ ] **Step 4: Run tests**

Run: `cd /home/mbow/code/search && go test ./bm25/ -v`

Expected: All tests pass including new `TestNewIndex_PrefixCap`.

- [ ] **Step 5: Regenerate embedded data**

Run: `cd /home/mbow/code/search && go run cmd/generate/main.go`

Expected: Output shows BM25 index built. The CBOR file should be smaller due to fewer prefix entries.

- [ ] **Step 6: Run full test suite**

Run: `cd /home/mbow/code/search && go test ./... -v`

Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add bm25/bm25.go bm25/bm25_test.go catalog/data.cbor
git commit -m "perf(bm25): cap word prefix length at 6 characters to reduce memory"
```

---

### Task 2: Shrink Hit Array from `[]int16` to `[]uint8`

**Files:**
- Modify: `index/ngram.go:47,105,189-237`
- Modify: `index/ngram_test.go`

- [ ] **Step 1: Write test for uint8 saturation**

Append to `index/ngram_test.go`:

```go
func TestSearchSaturationSafety(t *testing.T) {
	// Create a product with a very long name that produces many trigrams.
	// This tests that the uint8 hit counter doesn't overflow.
	longName := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
	products := []catalog.Product{
		{Name: longName, Category: "test"},
	}
	idx := NewIndex(products)

	// Searching with a long query that shares many trigrams should not panic
	results := idx.Search(longName[:30])
	if len(results) == 0 {
		t.Fatal("expected results for long query substring")
	}
}
```

- [ ] **Step 2: Run test to verify it passes (baseline — existing code handles this)**

Run: `cd /home/mbow/code/search && go test ./index/ -run TestSearchSaturationSafety -v`

Expected: PASS (current int16 handles it fine).

- [ ] **Step 3: Change `[]int16` to `[]uint8` with saturation**

Edit `index/ngram.go`:

1. Line 47 — change `hitsPool` type comment and the `NewIndex` pool factory (line 120 area):

Change the pool factory in `NewIndex` (around line 120):
```go
hitsPool: sync.Pool{New: func() any { s := make([]uint8, n); return &s }},
```

Also change in `FromSnapshot` (line 105):
```go
hitsPool: sync.Pool{New: func() any { s := make([]uint8, n); return &s }},
```

2. Lines 189-237 — update the `Search` method:

Change line 189:
```go
	hitsPtr := idx.hitsPool.Get().(*[]uint8)
```

Change line 190:
```go
	hits := *hitsPtr
```

Change line 199 (first posting loop):
```go
		if hits[id] < 255 {
			hits[id]++
		}
```

Change lines 206-211 (remaining posting loops):
```go
			if hits[id] > 0 {
				if hits[id] < 255 {
					hits[id]++
				}
			} else if expand {
				hits[id] = 1
				touched = append(touched, id)
			}
```

Change line 216:
```go
	minHits := uint8(max(1, numQueryGrams/3))
```

Change line 221:
```go
		h := hits[id]
```

- [ ] **Step 4: Run tests**

Run: `cd /home/mbow/code/search && go test ./index/ -v`

Expected: All tests pass including saturation safety test.

- [ ] **Step 5: Run full test suite**

Run: `cd /home/mbow/code/search && go test ./...`

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add index/ngram.go index/ngram_test.go
git commit -m "perf(index): shrink hit array from int16 to uint8 with saturation guard"
```

---

### Task 3: Pooled Bitset Dedup in BM25 Search

**Files:**
- Modify: `bm25/bm25.go:32-43,80-91,189-247,290-316`
- Modify: `bm25/bm25_test.go`

- [ ] **Step 1: Write benchmark to measure current map allocation**

Append to `bm25/bm25_test.go`:

```go
func BenchmarkBM25Search_CommonPrefix(b *testing.B) {
	// Use the full embedded catalog for realistic allocation measurement
	products, err := catalog.EmbeddedProducts()
	if err != nil {
		b.Skip("embedded products not available")
	}
	idx := NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("b")
	}
}
```

Add `"github.com/mbow/go-xsearch/catalog"` to the test file imports.

- [ ] **Step 2: Run benchmark to capture baseline**

Run: `cd /home/mbow/code/search && go test ./bm25/ -bench=BenchmarkBM25Search_CommonPrefix -benchmem -count=3`

Expected: Note the allocs/op — this is the map-based baseline.

- [ ] **Step 3: Add `sync.Pool` and `seenPool` to Index struct**

Edit `bm25/bm25.go`:

1. Add `"sync"` to imports (line 9-17).

2. Add pool field to `Index` struct (after line 42):
```go
	seenPool sync.Pool
```

3. In `NewIndex`, after setting `b` field (line 90), add:
```go
	idx.seenPool = sync.Pool{New: func() any { s := make([]bool, n); return &s }}
```

4. In `FromSnapshot`, after building the `Index` literal (before the `return`), add:
```go
	n := len(s.TermFreqs)
```
And add the pool to the returned Index:
```go
	idx := &Index{
		// ... existing fields ...
	}
	idx.seenPool = sync.Pool{New: func() any { s := make([]bool, n); return &s }}
	return idx, nil
```

- [ ] **Step 4: Replace map dedup with pooled bitset in Search**

Replace lines 197-206 of `bm25/bm25.go` (the `seen` map section) with:

```go
	// Use pooled bitset for O(1) dedup with zero allocation.
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
		// Clean up and return pool
		for _, id := range candidates {
			seen[id] = false
		}
		idx.seenPool.Put(seenPtr)
		return nil
	}
```

Replace `for id := range seen {` (line 223) with `for _, id := range candidates {`.

At the end of `Search`, before the return, add cleanup:

```go
	// Clear touched entries and return bitset to pool.
	for _, id := range candidates {
		seen[id] = false
	}
	idx.seenPool.Put(seenPtr)
```

Replace `results := make([]SearchResult, 0, len(seen))` with `results := make([]SearchResult, 0, min(len(candidates), 64))`.

- [ ] **Step 5: Run tests**

Run: `cd /home/mbow/code/search && go test ./bm25/ -v`

Expected: All tests pass.

- [ ] **Step 6: Run benchmark comparison**

Run: `cd /home/mbow/code/search && go test ./bm25/ -bench=BenchmarkBM25Search_CommonPrefix -benchmem -count=3`

Expected: allocs/op should be lower than the baseline captured in Step 2.

- [ ] **Step 7: Regenerate embedded data and run full tests**

Run: `cd /home/mbow/code/search && go run cmd/generate/main.go && go test ./...`

Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add bm25/bm25.go bm25/bm25_test.go catalog/data.cbor
git commit -m "perf(bm25): replace map dedup with pooled bitset for zero-alloc candidate collection"
```

---

### Task 4: Top-K Heap Select in BM25 Search

**Files:**
- Modify: `bm25/bm25.go:189-247`
- Modify: `bm25/bm25_test.go`

- [ ] **Step 1: Write benchmark for top-K vs full sort**

Append to `bm25/bm25_test.go`:

```go
func BenchmarkBM25Search_FullSort(b *testing.B) {
	products, err := catalog.EmbeddedProducts()
	if err != nil {
		b.Skip("embedded products not available")
	}
	idx := NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("bud")
	}
}
```

- [ ] **Step 2: Run benchmark baseline**

Run: `cd /home/mbow/code/search && go test ./bm25/ -bench=BenchmarkBM25Search_FullSort -benchmem -count=3`

Expected: Note ns/op — this is the full-sort baseline.

- [ ] **Step 3: Implement top-K heap**

Add to `bm25/bm25.go`, after the `SearchResult` type (line 30):

```go
// maxSearchResults is the maximum number of results returned by Search.
const maxSearchResults = 10

// resultHeap is a min-heap of SearchResult ordered by Score ascending.
// The smallest score is on top, making it efficient to evict the worst
// candidate when a better one is found.
type resultHeap []SearchResult

func (h resultHeap) Len() int            { return len(h) }
func (h resultHeap) Less(i, j int) bool  { return h[i].Score < h[j].Score }
func (h resultHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *resultHeap) Push(x any)         { *h = append(*h, x.(SearchResult)) }
func (h *resultHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
```

Add `"container/heap"` to imports.

- [ ] **Step 4: Replace full sort with heap select in Search**

In the `Search` method, replace the scoring + sorting section (from `results := make(...)` through the `slices.SortFunc` call) with:

```go
	// Use a min-heap of size maxSearchResults for top-K selection.
	// O(n * log(k)) instead of O(n * log(n)).
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

	// Extract results from heap in descending score order.
	results := make([]SearchResult, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(&h).(SearchResult)
	}
```

Remove the `"slices"` and `"cmp"` imports if they are no longer used elsewhere in the file. (Check first — `ToSnapshot` uses `slices.Sort`.) Keep `"slices"` if `ToSnapshot` still needs it. Remove `"cmp"` only if no other function uses it.

- [ ] **Step 5: Run tests**

Run: `cd /home/mbow/code/search && go test ./bm25/ -v`

Expected: All tests pass. Verify `TestSearch_PrefixBoost` still returns Budweiser/Bud Light in top 2.

- [ ] **Step 6: Run benchmark comparison**

Run: `cd /home/mbow/code/search && go test ./bm25/ -bench=BenchmarkBM25Search_FullSort -benchmem -count=3`

Expected: Should be faster than Step 2 baseline, especially on larger candidate sets.

- [ ] **Step 7: Run full test suite**

Run: `cd /home/mbow/code/search && go test ./...`

Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add bm25/bm25.go bm25/bm25_test.go
git commit -m "perf(bm25): replace full sort with top-K min-heap for O(n*log(k)) candidate selection"
```

---

### Task 5: Sorted Prefix Array for Binary Search Fallback

**Files:**
- Modify: `index/ngram.go:40-48,50-56,78-107,109-145,244-252`
- Modify: `index/ngram_test.go`

- [ ] **Step 1: Write test for binary search prefix**

Append to `index/ngram_test.go`:

```go
func TestPrefixSearchBinarySearch(t *testing.T) {
	products := []catalog.Product{
		{Name: "Apple Juice", Category: "drinks"},
		{Name: "Banana Split", Category: "dessert"},
		{Name: "Blueberry Muffin", Category: "bakery"},
		{Name: "Cherry Pie", Category: "bakery"},
	}
	idx := NewIndex(products)

	// "bl" should find "Blueberry Muffin" via binary search
	results := idx.Search("bl")
	found := false
	for _, r := range results {
		if r.ProductID == 2 { // Blueberry Muffin
			found = true
		}
	}
	if !found {
		t.Error("expected to find Blueberry Muffin for prefix 'bl'")
	}

	// "b" should find both Banana and Blueberry
	results = idx.Search("b")
	bCount := 0
	for _, r := range results {
		if r.ProductID == 1 || r.ProductID == 2 {
			bCount++
		}
	}
	if bCount < 2 {
		t.Errorf("expected Banana and Blueberry for prefix 'b', found %d", bCount)
	}
}
```

- [ ] **Step 2: Run test to verify it passes (existing linear scan works)**

Run: `cd /home/mbow/code/search && go test ./index/ -run TestPrefixSearchBinarySearch -v`

Expected: PASS.

- [ ] **Step 3: Add sorted names to Index struct**

Edit `index/ngram.go`:

1. Add a new type before the `Index` struct:

```go
// nameEntry pairs a lowercased product name with its product ID,
// used for binary search prefix matching on short queries.
type nameEntry struct {
	Name string
	ID   int
}
```

2. Add field to `Index` struct (after `hitsPool`):

```go
	sortedNames []nameEntry // sorted by Name for binary search prefix
```

3. Add to `Snapshot` struct:

```go
	SortedNames []nameEntry `cbor:"sorted_names"`
```

4. In `NewIndex`, after the main loop (before `return idx`), add:

```go
	// Build sorted name array for binary search prefix queries.
	idx.sortedNames = make([]nameEntry, n)
	for i, p := range products {
		idx.sortedNames[i] = nameEntry{Name: strings.ToLower(p.Name), ID: i}
	}
	slices.SortFunc(idx.sortedNames, func(a, b nameEntry) int {
		return cmp.Compare(a.Name, b.Name)
	})
```

Add `"cmp"` to imports if not already present.

5. In `ToSnapshot`, add `SortedNames: idx.sortedNames` to the return struct.

6. In `FromSnapshot`, add `sortedNames: s.SortedNames` to the returned `Index`.

- [ ] **Step 4: Replace linear prefixSearch with binary search**

Replace the `prefixSearch` method (lines 244-252) with:

```go
// prefixSearch uses binary search on the sorted name array to find
// products whose name starts with the given short query.
// O(log n + k) where k is the number of matches.
func (idx *Index) prefixSearch(query string) []SearchResult {
	n := len(idx.sortedNames)
	// Binary search for the first name >= query.
	lo := sort.Search(n, func(i int) bool {
		return idx.sortedNames[i].Name >= query
	})

	var results []SearchResult
	for i := lo; i < n; i++ {
		if !strings.HasPrefix(idx.sortedNames[i].Name, query) {
			break
		}
		results = append(results, SearchResult{
			ProductID: idx.sortedNames[i].ID,
			Score:     1.0,
		})
	}
	return results
}
```

Add `"sort"` to imports.

- [ ] **Step 5: Run tests**

Run: `cd /home/mbow/code/search && go test ./index/ -v`

Expected: All tests pass.

- [ ] **Step 6: Regenerate embedded data and run full suite**

Run: `cd /home/mbow/code/search && go run cmd/generate/main.go && go test ./...`

Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add index/ngram.go index/ngram_test.go catalog/data.cbor
git commit -m "perf(index): replace O(N) prefix scan with binary search on sorted names"
```

---

### Task 6: Parallel Prefix Cache Build

**Files:**
- Modify: `engine/engine.go:141-166`

- [ ] **Step 1: Replace sequential buildPrefixCache with parallel version**

Replace the `buildPrefixCache` method in `engine/engine.go` (lines 141-166) with:

```go
// buildPrefixCache precomputes search results for every 1-char and 2-char
// prefix that matches at least one product. Uses bounded goroutines for
// parallel computation at large catalog sizes.
func (e *Engine) buildPrefixCache() {
	// Collect unique 1-char and 2-char prefixes from product names.
	prefixSet := make(map[string]struct{})
	for _, p := range e.products {
		name := strings.ToLower(p.Name)
		if len(name) >= 1 {
			prefixSet[name[:1]] = struct{}{}
		}
		if len(name) >= 2 {
			prefixSet[name[:2]] = struct{}{}
		}
	}

	prefixes := make([]string, 0, len(prefixSet))
	for p := range prefixSet {
		prefixes = append(prefixes, p)
	}

	type entry struct {
		prefix  string
		results []Result
	}

	workers := runtime.GOMAXPROCS(0)
	ch := make(chan string, len(prefixes))
	resultCh := make(chan entry, len(prefixes))

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for prefix := range ch {
				results := e.searchInternal(prefix)
				if len(results) > 0 {
					resultCh <- entry{prefix: prefix, results: results}
				}
			}
		}()
	}

	for _, p := range prefixes {
		ch <- p
	}
	close(ch)
	wg.Wait()
	close(resultCh)

	e.prefixCache = make(map[string][]Result, len(prefixes))
	for ent := range resultCh {
		e.prefixCache[ent.prefix] = ent.results
	}
}
```

Add `"runtime"` to the imports in `engine/engine.go`.

- [ ] **Step 2: Run tests**

Run: `cd /home/mbow/code/search && go test ./... -v -race`

Expected: All tests pass with no race conditions detected.

- [ ] **Step 3: Commit**

```bash
git add engine/engine.go
git commit -m "perf(engine): parallelize prefix cache build across GOMAXPROCS workers"
```

---

### Task 7: Match Highlighting

**Files:**
- Modify: `engine/engine.go:21-35,293-337,339-355`
- Modify: `engine/engine_test.go`
- Modify: `templates/results.html`
- Modify: `main.go:27-40`

- [ ] **Step 1: Write test for highlighting**

Append to `engine/engine_test.go`:

```go
func TestSearchHighlighting(t *testing.T) {
	products := []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Bud Light", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
	}

	e := New(products)

	// BM25 path — "bud" should highlight "Bud" in "Budweiser"
	results := e.Search("bud")
	if len(results) == 0 {
		t.Fatal("expected results for 'bud'")
	}

	// First result should have a highlight
	first := results[0]
	if len(first.Highlights) == 0 {
		t.Error("expected highlights on first result")
	}
	if first.Highlights[0].Start != 0 {
		t.Errorf("expected highlight start at 0, got %d", first.Highlights[0].Start)
	}
	if first.Highlights[0].End != 3 {
		t.Errorf("expected highlight end at 3, got %d", first.Highlights[0].End)
	}
	if first.HighlightedName == "" {
		t.Error("expected non-empty HighlightedName")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/mbow/code/search && go test ./engine/ -run TestSearchHighlighting -v`

Expected: FAIL — `Highlights` and `HighlightedName` fields don't exist yet.

- [ ] **Step 3: Add Highlight types to engine.go**

Edit `engine/engine.go`. After the `MatchType` constants (line 27), add:

```go
// Highlight marks a matched byte range within a product name.
type Highlight struct {
	Start int // byte offset (inclusive)
	End   int // byte offset (exclusive)
}
```

Add fields to the `Result` struct (after `MatchType`):

```go
	Highlights      []Highlight // matched byte ranges in Product.Name
	HighlightedName string      // pre-rendered name with <mark> tags
```

- [ ] **Step 4: Add highlight computation functions**

Add after the `scorerFunc` type in `engine/engine.go`:

```go
// computeHighlights finds the byte positions of query matches in the product name.
func computeHighlights(name, query string) []Highlight {
	lowerName := strings.ToLower(name)
	lowerQuery := strings.ToLower(query)

	// Try to find each query word in the product name.
	words := strings.Fields(lowerQuery)
	var highlights []Highlight
	for _, word := range words {
		idx := strings.Index(lowerName, word)
		if idx >= 0 {
			highlights = append(highlights, Highlight{Start: idx, End: idx + len(word)})
		}
	}

	// If no word matches, try the full query as a substring.
	if len(highlights) == 0 {
		idx := strings.Index(lowerName, lowerQuery)
		if idx >= 0 {
			highlights = append(highlights, Highlight{Start: idx, End: idx + len(lowerQuery)})
		}
	}

	// Sort by start position and merge overlaps.
	slices.SortFunc(highlights, func(a, b Highlight) int {
		return cmp.Compare(a.Start, b.Start)
	})
	highlights = mergeHighlights(highlights)
	return highlights
}

// mergeHighlights merges overlapping or adjacent highlight ranges.
func mergeHighlights(hs []Highlight) []Highlight {
	if len(hs) <= 1 {
		return hs
	}
	merged := []Highlight{hs[0]}
	for _, h := range hs[1:] {
		last := &merged[len(merged)-1]
		if h.Start <= last.End {
			last.End = max(last.End, h.End)
		} else {
			merged = append(merged, h)
		}
	}
	return merged
}

// buildHighlightedName renders product name HTML with <mark> tags around highlights.
func buildHighlightedName(name string, highlights []Highlight) string {
	if len(highlights) == 0 {
		return template.HTMLEscapeString(name)
	}

	var b strings.Builder
	prev := 0
	for _, h := range highlights {
		if h.Start > prev {
			b.WriteString(template.HTMLEscapeString(name[prev:h.Start]))
		}
		b.WriteString("<mark>")
		b.WriteString(template.HTMLEscapeString(name[h.Start:h.End]))
		b.WriteString("</mark>")
		prev = h.End
	}
	if prev < len(name) {
		b.WriteString(template.HTMLEscapeString(name[prev:]))
	}
	return b.String()
}
```

Add `"html/template"` to imports (aliased or use the full path since `template` may conflict).

Actually, use `html.EscapeString` from `"html"` package instead:

```go
import "html"
```

And replace `template.HTMLEscapeString` with `html.EscapeString`.

- [ ] **Step 5: Populate highlights in buildBM25Results and buildResults**

In `buildBM25Results`, after setting `MatchType: MatchDirect,` add:

```go
		hl := computeHighlights(e.products[r.ProductID].Name, query)
```

Wait — `buildBM25Results` doesn't receive the query string. Change its signature:

```go
func (e *Engine) buildBM25Results(bm25Results []bm25.SearchResult, score scorerFunc, query string) []Result {
```

Update the caller in `searchInternal` (line 221):
```go
		return e.buildBM25Results(bm25Results, score, query)
```

Then in the loop, after `MatchType: MatchDirect,`:
```go
			hl := computeHighlights(e.products[r.ProductID].Name, query)
			// ...
			results = append(results, Result{
				// ... existing fields ...
				Highlights:      hl,
				HighlightedName: buildHighlightedName(e.products[r.ProductID].Name, hl),
			})
```

Similarly update `buildResults` to accept `query string`:

```go
func (e *Engine) buildResults(searchResults []index.SearchResult, matchType MatchType, score scorerFunc, query string) []Result {
```

And in the loop, compute and set highlights:

```go
		hl := computeHighlights(e.products[sr.ProductID].Name, query)
		results = append(results, Result{
			// ... existing fields ...
			Highlights:      hl,
			HighlightedName: buildHighlightedName(e.products[sr.ProductID].Name, hl),
		})
```

Update all callers of `buildResults` in `searchInternal` to pass `query`.

- [ ] **Step 6: Run tests**

Run: `cd /home/mbow/code/search && go test ./engine/ -v`

Expected: All tests pass including `TestSearchHighlighting`.

- [ ] **Step 7: Update results template**

Edit `templates/results.html`. Change line 9:

From: `<div class="result-name">{{.Product.Name}}</div>`
To: `<div class="result-name">{{.HighlightedName}}</div>`

And change line 23 similarly:

From: `<div class="result-name">{{.Product.Name}}</div>`
To: `<div class="result-name">{{.HighlightedName}}</div>`

Since `HighlightedName` contains HTML (`<mark>` tags), the template needs to output it unescaped. Change the Result template rendering in `main.go` — actually, `HighlightedName` is a `string`, so we need `template.HTML` type or use a template function.

The simplest approach: in `main.go`, register a template function `safe` that converts string to `template.HTML`:

In `loadTemplates` (main.go), change to:

```go
func (app *App) loadTemplates() {
	funcMap := template.FuncMap{
		"safe": func(s string) template.HTML { return template.HTML(s) },
	}
	app.indexTmpl = template.Must(template.New("index.html").Funcs(funcMap).ParseFiles("templates/index.html"))
	app.resultTmpl = template.Must(template.New("results.html").Funcs(funcMap).ParseFiles("templates/results.html"))
}
```

Then in `results.html`, use: `<div class="result-name">{{.HighlightedName | safe}}</div>`

- [ ] **Step 8: Add CSS for mark tags**

Edit `templates/index.html`. In the `<style>` section (around line 8), add:

```css
        mark { background: #fff3cd; padding: 0; font-weight: 500; }
```

- [ ] **Step 9: Run full tests**

Run: `cd /home/mbow/code/search && go test ./...`

Expected: All pass.

- [ ] **Step 10: Commit**

```bash
git add engine/engine.go engine/engine_test.go templates/results.html templates/index.html main.go
git commit -m "feat(engine): add match highlighting with <mark> tags in search results"
```

---

### Task 8: Ghost Text Completion

**Files:**
- Modify: `main.go:27-40,92-134`
- Modify: `templates/results.html`
- Modify: `templates/index.html`

- [ ] **Step 1: Add Ghost field to ResultsData**

Edit `main.go`. In the `ResultsData` struct (around line 37), add:

```go
	Ghost string
```

- [ ] **Step 2: Compute ghost text in handleSearch**

In `handleSearch` (main.go), after building `data` (around line 118), add:

```go
	// Ghost text: completion suffix from the top result.
	if len(results) > 0 {
		name := results[0].Product.Name
		lowerName := strings.ToLower(name)
		lowerQuery := strings.ToLower(query)
		if strings.HasPrefix(lowerName, lowerQuery) {
			data.Ghost = name[len(lowerQuery):]
		}
	}
```

- [ ] **Step 3: Update results template with data-ghost attribute**

Edit `templates/results.html`. Wrap the entire content in a container div with `data-ghost`:

```html
<div id="results-inner" data-ghost="{{.Ghost}}">
{{if .DirectResults}}
<div class="result-section">Results</div>
{{range .DirectResults}}
<div class="result-item"
     hx-post="/select"
     hx-vals='{"id": "{{.ProductID}}"}'
     hx-swap="none"
     hx-indicator="false">
    <div class="result-name">{{.HighlightedName | safe}}</div>
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
     hx-swap="none"
     hx-indicator="false">
    <div class="result-name">{{.HighlightedName | safe}}</div>
    <div class="result-category">{{.Product.Category}}</div>
</div>
{{end}}
{{end}}

{{if and (not .DirectResults) (not .FallbackResults)}}
{{if .Query}}
<div class="result-section">No results found</div>
{{end}}
{{end}}
</div>
```

- [ ] **Step 4: Add ghost text CSS and JS to index.html**

Edit `templates/index.html`. Replace the search input section (lines 31-44) with:

```html
    <div class="search-container">
        <span id="ghost" class="ghost-text"></span>
        <input type="search"
               id="search-input"
               name="q"
               placeholder="Search products..."
               autocomplete="off"
               hx-get="/search"
               hx-trigger="keyup changed delay:150ms, search"
               hx-target="#results"
               hx-swap="innerHTML"
               hx-push-url="false"
               hx-indicator="#spinner">
        <span id="spinner" class="htmx-indicator"> Searching...</span>
    </div>
    <div id="results"></div>
```

Add to the `<style>` section:

```css
        .search-container { position: relative; }
        .ghost-text {
            position: absolute; top: 0; left: 0;
            padding: 12px; font-size: 16px; color: #ccc;
            pointer-events: none; white-space: pre;
            font-family: system-ui, sans-serif;
            border: 2px solid transparent;
        }
        input[type="search"] {
            width: 100%; padding: 12px; font-size: 16px;
            border: 2px solid #ccc; border-radius: 8px;
            background: transparent; position: relative;
        }
```

Add a `<script>` block before `</body>`:

```html
    <script>
    document.body.addEventListener('htmx:afterSwap', function(evt) {
        if (evt.detail.target.id !== 'results') return;
        var inner = document.getElementById('results-inner');
        var ghost = document.getElementById('ghost');
        var input = document.getElementById('search-input');
        if (!inner || !ghost || !input) return;

        var suffix = inner.getAttribute('data-ghost') || '';
        // Pad ghost text to align after user's input text
        ghost.textContent = input.value + suffix;
    });

    document.getElementById('search-input').addEventListener('keydown', function(e) {
        if (e.key === 'Tab') {
            var ghost = document.getElementById('ghost');
            if (ghost.textContent && ghost.textContent !== this.value) {
                e.preventDefault();
                this.value = ghost.textContent;
                htmx.trigger(this, 'search');
            }
        }
    });
    </script>
```

- [ ] **Step 5: Run tests**

Run: `cd /home/mbow/code/search && go test ./...`

Expected: All pass. (Ghost text is a presentation feature — no new Go tests needed beyond the existing `handleSearch` tests.)

- [ ] **Step 6: Commit**

```bash
git add main.go templates/results.html templates/index.html
git commit -m "feat: add ghost text typeahead completion in search input"
```

---

### Task 9: Keyboard Navigation

**Files:**
- Modify: `templates/index.html`

- [ ] **Step 1: Add keyboard navigation JS and CSS**

Edit `templates/index.html`. Add to the `<style>` section:

```css
        .result-item.active { background: #e8f0fe; }
        .result-item { transition: background 0.1s; }
```

Extend the existing `<script>` block (add before the closing `</script>`):

```javascript
    // Keyboard navigation
    var activeIdx = -1;

    document.getElementById('search-input').addEventListener('keydown', function(e) {
        var items = document.querySelectorAll('.result-item');
        if (!items.length) return;

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            activeIdx = Math.min(activeIdx + 1, items.length - 1);
            updateActive(items);
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            activeIdx = Math.max(activeIdx - 1, -1);
            updateActive(items);
        } else if (e.key === 'Enter' && activeIdx >= 0) {
            e.preventDefault();
            var item = items[activeIdx];
            // Fill input with selected product name
            var nameEl = item.querySelector('.result-name');
            if (nameEl) this.value = nameEl.textContent;
            // Fire selection tracking
            htmx.trigger(item, 'click');
            activeIdx = -1;
            updateActive(items);
        } else if (e.key === 'Escape') {
            activeIdx = -1;
            updateActive(items);
        }
    });

    function updateActive(items) {
        items.forEach(function(el, i) {
            el.classList.toggle('active', i === activeIdx);
        });
    }

    // Reset keyboard state when new results arrive
    document.body.addEventListener('htmx:afterSwap', function(evt) {
        if (evt.detail.target.id === 'results') {
            activeIdx = -1;
        }
    });
```

Note: The Tab key handler from Task 8 already has its own `keydown` listener. Merge both into a single listener. The combined listener should handle Tab (ghost text), ArrowDown, ArrowUp, Enter, and Escape.

Refactor both into one listener:

```javascript
    var activeIdx = -1;

    document.getElementById('search-input').addEventListener('keydown', function(e) {
        var items = document.querySelectorAll('.result-item');

        if (e.key === 'Tab') {
            var ghost = document.getElementById('ghost');
            if (ghost.textContent && ghost.textContent !== this.value) {
                e.preventDefault();
                this.value = ghost.textContent;
                htmx.trigger(this, 'search');
            }
            return;
        }

        if (!items.length) return;

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            activeIdx = Math.min(activeIdx + 1, items.length - 1);
            updateActive(items);
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            activeIdx = Math.max(activeIdx - 1, -1);
            updateActive(items);
        } else if (e.key === 'Enter' && activeIdx >= 0) {
            e.preventDefault();
            var item = items[activeIdx];
            var nameEl = item.querySelector('.result-name');
            if (nameEl) this.value = nameEl.textContent;
            htmx.trigger(item, 'click');
            activeIdx = -1;
            updateActive(items);
        } else if (e.key === 'Escape') {
            activeIdx = -1;
            updateActive(items);
        }
    });

    function updateActive(items) {
        items.forEach(function(el, i) {
            el.classList.toggle('active', i === activeIdx);
        });
    }
```

- [ ] **Step 2: Test manually**

Run: `cd /home/mbow/code/search && go run .`

Open `http://localhost:8080` in browser. Verify:
- Type "bud" — results appear with highlighted text, ghost text shows "weiser"
- Press Tab — input fills with "Budweiser", new search fires
- Press ArrowDown — first result highlights with blue background
- Press ArrowDown again — second result highlights
- Press ArrowUp — back to first
- Press Enter — product selected, input fills with name
- Press Escape — selection cleared
- No layout shift at any point

- [ ] **Step 3: Run full tests**

Run: `cd /home/mbow/code/search && go test ./...`

Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add templates/index.html
git commit -m "feat: add keyboard navigation (arrow keys, Enter, Escape) for typeahead results"
```

---

### Task 10: After Benchmarks and Comparison

**Files:**
- Create: `docs/benchmarks/after-perf-typeahead.txt`

- [ ] **Step 1: Run full benchmark suite**

Run: `cd /home/mbow/code/search && go test -bench=. -benchmem -count=6 ./... 2>&1 | tee docs/benchmarks/after-perf-typeahead.txt`

Expected: All benchmarks pass.

- [ ] **Step 2: Run benchstat comparison**

Run: `cd /home/mbow/code/search && benchstat docs/benchmarks/baseline-pre-perf.txt docs/benchmarks/after-perf-typeahead.txt`

Expected: Improvements on BM25 search paths, no regression on existing paths.

- [ ] **Step 3: Commit benchmark results**

```bash
git add docs/benchmarks/after-perf-typeahead.txt
git commit -m "bench: capture after-perf-typeahead benchmarks for regression comparison"
```

---

### Task 11: Go Quality Gate

Run all relevant Go skills across new and modified files as a final quality pass.

**Files to review:**
- `bm25/bm25.go`
- `bm25/bm25_test.go`
- `index/ngram.go`
- `index/ngram_test.go`
- `engine/engine.go`
- `engine/engine_test.go`
- `main.go`
- `templates/results.html`
- `templates/index.html`

- [ ] **Step 1: Run Go skills**

Invoke all relevant Go skills in sequence.

- [ ] **Step 2: Apply fixes and run tests**

Run: `cd /home/mbow/code/search && go test ./... -v`

- [ ] **Step 3: Final commit**

```bash
git add -A
git commit -m "refactor: apply Go quality gate improvements to perf + typeahead changes"
```

- [ ] **Step 4: Final verification**

Run: `cd /home/mbow/code/search && go test ./... -v && go test -bench=. -benchmem ./...`

Expected: All tests pass, all benchmarks run.
