# Allocation Reduction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce GC pressure from 35% to ~15-20% of CPU time by eliminating allocations identified in profiling. Seven fixes targeting the BM25 hot path, highlight computation, and HTTP rendering.

**Architecture:** Profile-driven: each fix targets a specific allocator identified by `pprof alloc_objects`. Changes are independent — each can be benchmarked in isolation.

**Tech Stack:** Go 1.26, `sync.Pool`, `strings.FieldsSeq`, manual min-heap, sorted slices

**Spec:** `docs/superpowers/specs/2026-04-01-perf-profiling-findings.md`

---

### Task 0: Record Baseline

- [ ] **Step 1: Record benchmarks at current commit**

```bash
make bench-record
```

- [ ] **Step 2: Save as comparison point**

```bash
make bench-save
```

- [ ] **Step 3: Commit**

```bash
git add docs/benchmarks/
git commit -m "bench: save baseline before allocation reduction work"
```

---

### Task 1: Replace `container/heap` with Typed Min-Heap

**Files:**
- Modify: `bm25/bm25.go:34-52,233-263`
- Test: `bm25/bm25_test.go`

Eliminates `any` interface boxing on every Push/Pop (8.1% of allocs).

- [ ] **Step 1: Replace the `container/heap` interface with typed functions**

Remove the `resultHeap` type and its 5 interface methods (lines 34-52). Remove the `"container/heap"` import. Replace with:

```go
// siftDown restores the min-heap property for results[lo:hi] starting at position lo.
func siftDown(results []SearchResult, root, n int) {
	for {
		child := 2*root + 1
		if child >= n {
			break
		}
		// Pick the smaller child.
		if child+1 < n && lessResult(results[child+1], results[child]) {
			child++
		}
		if !lessResult(results[child], results[root]) {
			break
		}
		results[root], results[child] = results[child], results[root]
		root = child
	}
}

// lessResult returns true if a has lower priority than b (smaller score, or same score with higher ID).
func lessResult(a, b SearchResult) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return a.ProductID > b.ProductID
}
```

- [ ] **Step 2: Update `Search` to use typed heap operations**

Replace the heap section in `Search` (the `h := make(resultHeap, ...)` through the drain loop) with:

```go
	// Use a typed min-heap of size maxSearchResults — no interface boxing.
	h := make([]SearchResult, 0, maxSearchResults)
	hLen := 0

	for _, id := range candidates {
		score := idx.Score(id, queryTerms)
		pm := idx.HasPrefixMatch(id, queryTerms)
		if pm {
			score += prefixBonus
		}
		if score <= 0 {
			continue
		}

		r := SearchResult{ProductID: id, Score: score, PrefixMatch: pm}
		if hLen < maxSearchResults {
			h = append(h, r)
			hLen++
			// Sift up.
			for i := hLen - 1; i > 0; {
				parent := (i - 1) / 2
				if !lessResult(h[i], h[parent]) {
					break
				}
				h[i], h[parent] = h[parent], h[i]
				i = parent
			}
		} else if score > h[0].Score {
			h[0] = r
			siftDown(h, 0, hLen)
		}
	}

	// Drain heap in descending score order.
	results := make([]SearchResult, hLen)
	for i := hLen - 1; i >= 0; i-- {
		results[i] = h[0]
		h[0] = h[hLen-1]
		hLen--
		siftDown(h, 0, hLen)
	}
```

- [ ] **Step 3: Run tests**

Run: `cd /home/mbow/code/search && go test ./bm25/ -v`

Expected: All tests pass. Verify `TestSearch_PrefixBoost` still returns Budweiser/Bud Light in top 2.

- [ ] **Step 4: Run benchmark comparison**

Run: `cd /home/mbow/code/search && go test -bench=BenchmarkBM25Search -benchmem -count=3 ./bm25/`

Expected: allocs/op should drop (no more interface boxing).

- [ ] **Step 5: Commit**

```bash
git add bm25/bm25.go
git commit -m "perf(bm25): replace container/heap with typed min-heap to eliminate interface boxing"
```

---

### Task 2: Pre-Lowercase Product Names

**Files:**
- Modify: `engine/engine.go:48-59,316-343,387-421,460-474,510-560`
- Test: `engine/engine_test.go`

Eliminates `strings.ToLower(name)` per-result in `computeHighlights` (20.2% of allocs).

- [ ] **Step 1: Add `lowerNames` slice to Engine**

In the `Engine` struct, add after `products`:

```go
	lowerNames []string // pre-lowered product names, indexed by product ID
```

- [ ] **Step 2: Build `lowerNames` in constructors**

In `New()`, after `bm25idx: bm25.NewIndex(products),` add:

```go
	lowerNames := make([]string, len(products))
	for i, p := range products {
		lowerNames[i] = strings.ToLower(p.Name)
	}
```

And add `lowerNames: lowerNames,` to the Engine init.

In `NewFromEmbedded()`, do the same after building the Engine, before `buildPrefixCache`:

```go
	e.lowerNames = make([]string, len(products))
	for i, p := range products {
		e.lowerNames[i] = strings.ToLower(p.Name)
	}
```

- [ ] **Step 3: Change `computeHighlights` to accept pre-lowered name**

Change signature from:
```go
func computeHighlights(name string, queryWords []string, query string) []Highlight {
	lowerName := strings.ToLower(name)
```

To:
```go
func computeHighlights(lowerName string, queryWords []string, query string) []Highlight {
```

Remove the `strings.ToLower(name)` line inside the function.

- [ ] **Step 4: Update all callers to pass `e.lowerNames[id]`**

In `buildBM25Results`, the highlight call (around line 401):
```go
		hl := computeHighlights(e.lowerNames[r.ProductID], queryWords, query)
```

In `addHighlights`:
```go
func (e *Engine) addHighlights(results []Result, query string) {
```
Make it a method on Engine so it can access `lowerNames`. Update the call:
```go
		hl := computeHighlights(e.lowerNames[results[i].ProductID], queryWords, query)
```

Update all callers of `addHighlights(results, query)` to `e.addHighlights(results, query)`.

In `rebuildCategoryCache`, the highlight call:
```go
		hl := computeHighlights(e.lowerNames[id], catWords, cat)
```

- [ ] **Step 5: Run tests**

Run: `cd /home/mbow/code/search && go test ./engine/ -v`

Expected: All pass including `TestSearchHighlighting`.

- [ ] **Step 6: Commit**

```bash
git add engine/engine.go engine/engine_test.go
git commit -m "perf(engine): pre-lowercase product names to avoid per-result strings.ToLower"
```

---

### Task 3: Pool Candidates Slice in BM25 Search

**Files:**
- Modify: `bm25/bm25.go:54-66,171,195-220`

Pools the `[]int` candidates slice alongside the `seen` bitset.

- [ ] **Step 1: Add `candidatesPool` to Index struct**

Add after `seenPool`:
```go
	candidatesPool sync.Pool
```

- [ ] **Step 2: Initialize pool in `NewIndex` and `FromSnapshot`**

After the `seenPool` init, add:
```go
	idx.candidatesPool = sync.Pool{New: func() any { s := make([]int, 0, 64); return &s }}
```

Same in `FromSnapshot`.

- [ ] **Step 3: Use pooled candidates in `Search`**

Replace `var candidates []int` with:
```go
	candidatesPtr := idx.candidatesPool.Get().(*[]int)
	candidates := (*candidatesPtr)[:0]
```

At the end, before returning, add:
```go
	*candidatesPtr = candidates
	idx.candidatesPool.Put(candidatesPtr)
```

- [ ] **Step 4: Run tests**

Run: `cd /home/mbow/code/search && go test ./bm25/ -v`

- [ ] **Step 5: Commit**

```bash
git add bm25/bm25.go
git commit -m "perf(bm25): pool candidates slice to avoid per-search allocation"
```

---

### Task 4: Use `strings.FieldsSeq` in Tokenize

**Files:**
- Modify: `bm25/bm25.go:68-88`
- Test: `bm25/bm25_test.go`

Eliminates the `[]string` allocation from `strings.Fields` (7.0% of allocs).

- [ ] **Step 1: Rewrite Tokenize to use FieldsSeq**

Replace the `Tokenize` function with:

```go
// Tokenize splits s on whitespace, lowercases each token, and filters out
// pure-punctuation tokens (keeps tokens with at least one alphanumeric char).
// Uses strings.FieldsSeq to avoid allocating an intermediate []string.
func Tokenize(s string) []string {
	var tokens []string
	for field := range strings.FieldsSeq(s) {
		lower := strings.ToLower(field)
		if hasAlphanumeric(lower) {
			tokens = append(tokens, lower)
		}
	}
	return tokens
}
```

Note: This still allocates the result `[]string` but avoids the intermediate `strings.Fields` allocation. The total allocation count drops by 1 per call.

- [ ] **Step 2: Run tests**

Run: `cd /home/mbow/code/search && go test ./bm25/ -run TestTokenize -v`

Expected: All tokenize tests still pass.

- [ ] **Step 3: Commit**

```bash
git add bm25/bm25.go
git commit -m "perf(bm25): use strings.FieldsSeq in Tokenize to avoid intermediate slice"
```

---

### Task 5: Stack-Allocate Highlight Buffer

**Files:**
- Modify: `engine/engine.go:319-343`

Avoids escape to heap for the common case of 1-4 highlights.

- [ ] **Step 1: Use stack-allocated array in `computeHighlights`**

Replace:
```go
	var highlights []Highlight
```

With:
```go
	var buf [4]Highlight
	highlights := buf[:0]
```

Also in `mergeHighlights`, replace:
```go
	merged := []Highlight{hs[0]}
```

With:
```go
	var buf [4]Highlight
	buf[0] = hs[0]
	merged := buf[:1]
```

- [ ] **Step 2: Run tests**

Run: `cd /home/mbow/code/search && go test ./engine/ -run TestSearchHighlighting -v`

- [ ] **Step 3: Verify escape analysis**

Run: `go build -gcflags="-m" ./engine/ 2>&1 | grep computeHighlights`

Expected: `buf` should NOT appear in "escapes to heap" output (stays on stack).

- [ ] **Step 4: Commit**

```bash
git add engine/engine.go
git commit -m "perf(engine): stack-allocate highlight buffer to avoid heap escape"
```

---

### Task 6: Replace Per-Product Maps with Sorted Slices in BM25

**Files:**
- Modify: `bm25/bm25.go:54-66,95-173,176-197,199-213,249-285,287-316`
- Modify: `bm25/bm25_test.go`
- Regenerate: `catalog/data.cbor`

Replaces 20,000 map allocations with compact sorted slices.

- [ ] **Step 1: Define new types**

Add before the `Index` struct:

```go
// TermFreq pairs a term with its frequency count in a document.
type TermFreq struct {
	Term  string `cbor:"t"`
	Count int    `cbor:"c"`
}
```

- [ ] **Step 2: Change Index struct**

Replace:
```go
	termFreqs      []map[string]int
	...
	wordPrefixes   []map[string]struct{}
```

With:
```go
	termFreqs      [][]TermFreq          // per-product sorted by Term
	...
	wordPrefixes   [][]string            // per-product sorted word prefixes
```

- [ ] **Step 3: Update `NewIndex` to build sorted slices**

Replace the `termFreqs` building section:
```go
		// Term frequencies — build sorted slice instead of map.
		tfMap := make(map[string]int, len(tokens))
		for _, tok := range tokens {
			tfMap[tok]++
		}
		tfs := make([]TermFreq, 0, len(tfMap))
		for term, count := range tfMap {
			tfs = append(tfs, TermFreq{Term: term, Count: count})
		}
		slices.SortFunc(tfs, func(a, b TermFreq) int { return cmp.Compare(a.Term, b.Term) })
		idx.termFreqs[i] = tfs
```

Replace the `wordPrefixes` building section:
```go
		// Word prefixes — sorted string slice instead of map.
		prefixMap := make(map[string]struct{})
		for _, word := range nameTokens {
			runes := []rune(word)
			maxPfx := min(len(runes), 6)
			for length := 1; length <= maxPfx; length++ {
				prefixMap[string(runes[:length])] = struct{}{}
			}
		}
		pfxSlice := make([]string, 0, len(prefixMap))
		for p := range prefixMap {
			pfxSlice = append(pfxSlice, p)
		}
		slices.Sort(pfxSlice)
		idx.wordPrefixes[i] = pfxSlice
```

- [ ] **Step 4: Update `Score` for sorted-slice lookup**

Replace:
```go
	tf := idx.termFreqs[productID]
	...
	f := float64(tf[term])
```

With:
```go
	tfs := idx.termFreqs[productID]
	...
	f := 0.0
	if j, found := slices.BinarySearchFunc(tfs, term, func(tf TermFreq, target string) int {
		return cmp.Compare(tf.Term, target)
	}); found {
		f = float64(tfs[j].Count)
	}
```

Add `"cmp"` to imports if not present.

- [ ] **Step 5: Update `HasPrefixMatch` for sorted-slice lookup**

Replace:
```go
	prefixes := idx.wordPrefixes[productID]
	for _, term := range queryTerms {
		if _, ok := prefixes[term]; ok {
```

With:
```go
	prefixes := idx.wordPrefixes[productID]
	for _, term := range queryTerms {
		if _, found := slices.BinarySearch(prefixes, term); found {
```

- [ ] **Step 6: Update Snapshot types**

Change `Snapshot`:
```go
	TermFreqs    [][]TermFreq `cbor:"term_freqs"`
	...
	WordPrefixes [][]string   `cbor:"word_prefixes"`
```

- [ ] **Step 7: Update `ToSnapshot` and `FromSnapshot`**

`ToSnapshot` — `termFreqs` and `wordPrefixes` are already sorted slices, so they can be passed directly:
```go
	return Snapshot{
		...
		TermFreqs:    idx.termFreqs,
		...
		WordPrefixes: idx.wordPrefixes,
		...
	}
```

`FromSnapshot` — they can be used directly:
```go
	idx := &Index{
		...
		termFreqs:     s.TermFreqs,
		...
		wordPrefixes:  s.WordPrefixes,
		...
	}
```

Remove the old conversion loops that converted maps ↔ slices.

- [ ] **Step 8: Regenerate data and run tests**

```bash
cd /home/mbow/code/search && go run cmd/generate/main.go && go test ./...
```

- [ ] **Step 9: Commit**

```bash
git add bm25/bm25.go bm25/bm25_test.go catalog/data.cbor
git commit -m "perf(bm25): replace per-product maps with sorted slices (eliminates 20K map allocs)"
```

---

### Task 7: Pool `bytes.Buffer` in Template Rendering

**Files:**
- Modify: `internal/server/server.go:66-120`

Pools the template rendering buffer to avoid per-request byte slice growth.

- [ ] **Step 1: Add buffer pool to App**

Add to the `App` struct:
```go
	bufPool sync.Pool
```

In `New()`, initialize it:
```go
	app.bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
```

- [ ] **Step 2: Use pooled buffer in `HandleSearch`**

Replace:
```go
	var buf bytes.Buffer
	if err := app.resultTmpl.Execute(&buf, data); err != nil {
```

With:
```go
	buf := app.bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer app.bufPool.Put(buf)

	if err := app.resultTmpl.Execute(buf, data); err != nil {
```

- [ ] **Step 3: Run tests**

Run: `cd /home/mbow/code/search && go test ./internal/server/ -v`

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go
git commit -m "perf(server): pool bytes.Buffer for template rendering"
```

---

### Task 8: Benchmark Comparison

- [ ] **Step 1: Record new benchmarks**

```bash
make bench-record
```

- [ ] **Step 2: Compare against baseline**

```bash
make bench-compare
```

Expected: Significant alloc reduction on BM25Path, PrefixBoost, and ColdCache paths.

- [ ] **Step 3: Commit results**

```bash
git add docs/benchmarks/
git commit -m "bench: capture results after allocation reduction work"
```
