# Performance Profiling Findings — 2026-04-01

**Date**: 2026-04-01
**Method**: `go test -bench -cpuprofile -memprofile` on BM25Path, Fuzzy, and PrefixBoost benchmarks, plus `go build -gcflags="-m -m"` escape analysis.

## Current State

| Benchmark | Time | Allocs | B/op |
|---|---|---|---|
| BM25Path | 3,828 ns | 20 | 3,137 B |
| Fuzzy (Jaccard) | 2,952 ns | 19 | 2,543 B |
| PrefixBoost | 4,134 ns | 65 | 3,940 B |

## CPU Profile

**GC consumes 34.7% of CPU time.** Every allocation reduction directly improves throughput.

| Function | Cumulative CPU % | Notes |
|---|---|---|
| GC (`gcDrain` + `scanObject`) | 34.7% | Dominant — driven by allocation count |
| `index.Search` (Jaccard) | 12.5% | Trigram posting list processing |
| `categoryFallback` + `SearchCategories` | 12.6% | Called on Fuzzy path when < 3 good results |
| `bm25.Search` | 8.9% | Candidate collection + heap scoring |
| `buildBM25Results` | 9.5% | Includes highlight computation |
| `mapaccess2_faststr` | 10.9% | IDF/termFreq lookups — unavoidable |
| `strings.ToLower` | varies | Called per-result in `computeHighlights` |

## Memory Profile (alloc_objects)

| # | Allocator | % of total | Per-search allocs | Root cause |
|---|---|---|---|---|
| 1 | `bytealg.MakeNoZero` (slice growth) | 20.3% | ~5 | Dynamic slice appends without preallocation |
| 2 | `bm25.Search` (candidates + heap) | 32.7% | ~7 | `[]int` candidates grows + `container/heap` interface boxing |
| 3 | `computeHighlights` | 20.2% | ~4 | `strings.ToLower(name)` allocates a new string per result |
| 4 | `resultHeap.Pop` | 8.1% | ~2 | `container/heap` boxes `SearchResult` into `any` on every Pop/Push |
| 5 | `strings.Fields` (Tokenize) | 7.0% | ~1 | Called in `bm25.Search` — allocates `[]string` per query |
| 6 | `Ranker.Scorer` | 5.4% | ~1 | Closure + map snapshot — already lazy, hard to avoid |
| 7 | `index.ExtractTrigrams` | 6.3% | ~1 | `[]string` allocation — already optimized |

## Escape Analysis (key findings)

From `go build -gcflags="-m -m"`:

**bm25/bm25.go:**
- `resultHeap.Push`: `append(*h, x.(SearchResult))` escapes to heap — the `any` interface causes boxing
- `resultHeap.Pop`: return value escapes — boxing again
- `Tokenize`: `make([]string, 0, len(fields))` escapes — allocates per call

**engine/engine.go:**
- `computeHighlights`: `[]Highlight` appends escape to heap — 2-4 allocs per call
- `buildHighlightedName`: `strings.Builder` internal buffer escapes — 1-2 allocs per call
- `mergeHighlights`: `[]Highlight{hs[0]}` escapes — 1 alloc per call

## Proposed Fixes (ordered by ROI)

### Fix 1: Replace `container/heap` with manual min-heap (eliminates 8.1% of allocs)

**Problem**: `container/heap` uses an `any` interface — every `Push` and `Pop` boxes `SearchResult` into a heap-allocated interface value. With 10 results, that's 10+ boxing allocations.

**Fix**: Replace with a typed min-heap that operates directly on `[]SearchResult`. No interface, no boxing. The heap logic is ~15 lines (siftUp, siftDown, swap).

**Expected impact**: -2 allocs/op on BM25Path, -10+ allocs/op on PrefixBoost (more candidates).

**Files**: `bm25/bm25.go`

### Fix 2: Pre-lowercase product names to avoid per-result `strings.ToLower` (eliminates 20.2% of allocs)

**Problem**: `computeHighlights` calls `strings.ToLower(name)` for every result. For 10 results, that's 10 string allocations (~500 bytes).

**Fix**: Store pre-lowered product names in the engine at build time. Pass the pre-lowered name to `computeHighlights` instead of lowering on every call.

**Expected impact**: -10 allocs on any path that highlights 10 results. The BM25Path and PrefixBoost benchmarks would see the biggest improvement.

**Files**: `engine/engine.go`, `catalog/catalog.go` (add `LowerName` field or separate slice)

### Fix 3: Pool the candidates slice in `bm25.Search` (reduces 32.7% of allocs)

**Problem**: `var candidates []int` in `bm25.Search` grows dynamically via append. For common prefixes ("b" in a beer catalog), this can grow to thousands of entries, causing multiple slice regrowths.

**Fix**: Pool a `[]int` alongside the existing `seenPool`. Get from pool, use, clear, return.

**Expected impact**: -1-2 allocs/op, more significant at scale (100K products where candidate lists are larger).

**Files**: `bm25/bm25.go`

### Fix 4: Avoid `strings.Fields` in `bm25.Tokenize` (eliminates 7.0% of allocs)

**Problem**: `strings.Fields` allocates a new `[]string` on every call. `Tokenize` is called once per `bm25.Search`.

**Fix**: Use `strings.FieldsSeq` (Go 1.24+) to iterate without allocating, or pool a reusable `[]string` buffer.

**Expected impact**: -1 alloc/op per BM25 search.

**Files**: `bm25/bm25.go`

### Fix 5: Pre-size `[]Highlight` in `computeHighlights` (reduces escape pressure)

**Problem**: `var highlights []Highlight` grows via append, causing escape to heap. Typical result has 1-2 highlights.

**Fix**: Use a small stack-allocated array: `var buf [4]Highlight; highlights := buf[:0]`. If the slice stays within 4 elements, it won't escape to heap.

**Expected impact**: -1 alloc/op per highlighted result.

**Files**: `engine/engine.go`

## Estimated Combined Impact

If all 5 fixes are applied:

| Benchmark | Current allocs | Est. after | Reduction |
|---|---|---|---|
| BM25Path | 20 | ~12-14 | -30-40% |
| Fuzzy | 19 | ~14-16 | -15-25% |
| PrefixBoost | 65 | ~45-50 | -23-30% |

GC CPU overhead would drop from ~35% to ~20-25%, yielding a ~15-20% wall-clock improvement across all search paths.

## What NOT to Optimize

| Area | Why leave it |
|---|---|
| `mapaccess2_faststr` (IDF lookups) | Map access is the fundamental data structure — can't avoid it without changing to arrays |
| `Ranker.Scorer` closure | Already lazy, creates 1 closure per search — unavoidable cost of decoupled scoring |
| `index.ExtractTrigrams` | Single allocation of `[]string` — already minimal |
| `categoryFallback` | Only runs when < 3 direct results — not the common path |
