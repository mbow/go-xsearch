# Performance: Future Optimizations for Scale

These are bottlenecks identified via pprof benchmarking (2026-03-31) that are
**not worth fixing at 226 products** but will become critical at 100k+ products
and categories.

## 1. Trigram Intersection Uses Map Lookups (29% CPU at 226 products)

**Where:** `index/ngram.go` — `Search()` Jaccard scoring loop

**Problem:** For each candidate product, we iterate query trigrams and check
`map[string]struct{}` membership in the product's trigram set. `mapaccess2_faststr`
(Go's internal fast-path string map lookup) is 29% of CPU time in the fuzzy
search benchmark. At 100k products with longer names, this dominates.

**Fix when needed:**
- Replace `map[string]struct{}` trigram sets with **sorted `[]string` slices**
- Jaccard intersection becomes a merge-intersect of two sorted slices: O(n+m)
  with zero hash overhead
- Or: assign each unique trigram an integer ID at index build time, store trigram
  sets as sorted `[]int` slices — integer comparison is faster than string comparison
- Or: represent trigram sets as bitsets (if total unique trigrams < 64k, which is
  likely) — intersection becomes `popcount(a & b)`

**Expected impact:** 2-5x speedup on the Jaccard scoring hot loop.

---

## 2. ExtractTrigrams Allocates Per Call (590MB cumulative in benchmarks)

**Where:** `index/ngram.go` — `ExtractTrigrams()`

**Problem:** Every call allocates a new `[]string` slice. On the search path,
this is called once per query (small), but at index build time with 100k products,
the allocations become significant. The GC handles it fine at 226 products.

**Fix when needed:**
- Accept a `buf []string` parameter and append into it (caller provides reusable buffer)
- Or: use `sync.Pool` for trigram slices
- Or: avoid materializing trigrams entirely — compute them inline during index
  lookup via a sliding window, never allocating a slice at all

**Expected impact:** Near-zero alloc on the search hot path.

---

## 3. Category Fallback Scores All Products in Category (17KB alloc, 8ms)

**Where:** `engine/engine.go` — `categoryFallback()`

**Problem:** When fallback triggers for "beer" (100 products), we score all 100
products, sort them, then return top 10. At 100k products per category, this
becomes O(n log n) sort on a large slice.

**Fix when needed:**
- Precompute top-N products per category at startup (same pattern as prefix cache)
- Refresh the precomputed lists when popularity data changes
- Or: use a min-heap of size 10 instead of full sort — O(n log 10) ≈ O(n)
- Or: maintain a pre-sorted popularity index per category, updated incrementally
  on each selection

**Expected impact:** Category fallback from O(n log n) to O(n) or O(1) with
precomputation.

---

## 4. Posting List Union Scales Linearly With Matches

**Where:** `index/ngram.go` — `Search()` bitset candidate collection

**Problem:** Currently we iterate every posting list for every query trigram and
set bits. With 100k products, popular trigrams like "the" or "pro" will have
posting lists with tens of thousands of entries. The bitset itself grows to
~12KB (100k/8 bits) and the forEach scan touches every word.

**Fix when needed:**
- Sort posting lists by product ID and use skip pointers for faster intersection
- Or: use Roaring Bitmaps (compressed bitsets that skip empty regions)
- Or: truncate posting lists to top-N by pre-ranked popularity — don't even
  consider low-popularity products for common trigrams
- Consider sharding the index by category to reduce candidate set size

**Expected impact:** 5-10x reduction in candidate collection time for common
trigrams.

---

## 5. Bloom Filter Becomes Less Useful at Scale

**Where:** `bloom/bloom.go`

**Problem:** At 226 products with 20k bits, false positive rate is <1%. At 100k
products generating ~500k unique trigrams, either the bit array must grow to
~5MB (for <1% FP) or the false positive rate climbs and the filter stops
rejecting anything useful.

**Fix when needed:**
- Scale bloom filter size proportionally: 10 bits per element with k=7 gives
  ~0.8% false positive rate
- Or: replace with a Cuckoo filter (better space efficiency at low FP rates)
- Or: remove the bloom filter entirely if the n-gram index lookup is fast enough
  (profile to check whether bloom actually saves time vs just querying the index)

---

## 6. Vector Search Becomes Necessary

**Where:** Conceptual — not in current codebase

**Problem:** At 100k products, users will search by description/intent ("something
smooth for a party", "gift for dad") not just product names. No amount of trigram
matching or category tags handles semantic queries.

**When to add:** When user feedback shows queries that can't be served by
name/category/tag matching. The current architecture supports this cleanly:
add a vector index as another candidate source in the engine orchestrator,
alongside bloom+ngram+category fallback.

---

## Benchmarks Baseline (2026-03-31, 226 products, AMD Ryzen 9 5950X)

```
BloomMayContain          6.2ns    0 allocs
ExtractTrigrams(9ch)      48ns    1 alloc
IndexSearch_Prefix       304ns    4 allocs
IndexSearch_Fuzzy        744ns    4 allocs
IndexSearch_Exact       1347ns    5 allocs
EngineSearch_Fuzzy      2762ns   10 allocs
EngineSearch_CatFallback 8220ns  20 allocs
EngineSearch_CachedPfx    12ns    0 allocs
HTTPSearch_WarmCache    2390ns   22 allocs
HTTPSearch_ColdCache   27000ns   89 allocs
```

Revisit these numbers when product count reaches 10k+ to see which bottlenecks
have become real problems.
