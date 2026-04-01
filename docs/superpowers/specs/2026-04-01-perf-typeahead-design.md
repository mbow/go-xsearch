# Performance at 100K Scale + Typeahead UX Design

**Date**: 2026-04-01
**Problem**: Current implementation benchmarked at 10K products. At 100K, several hot paths degrade: BM25 sorts all candidates instead of top-K, map allocations cause GC pressure, prefix data structures bloat, and short-query fallback is O(N). Additionally, the search UI lacks the polished "typeahead" feel — no match highlighting, no ghost text completion, no keyboard navigation.
**Solution**: Six targeted performance fixes for 100K scale, plus three frontend UX enhancements for a production-quality typeahead experience.

## Competitor Analysis Context

A competitor design proposed Weighted Radix Trie + Levenshtein edit distance + N-gram Markov chain. Our comparative assessment for a product catalog autocomplete:

| Feature | Competitor | Us | Verdict |
|---|---|---|---|
| Prefix matching | Radix Trie O(k) traversal | Precomputed prefix cache O(1) + BM25 prefix posting O(matches) | **Us 9/10** |
| Fuzzy/typo | Levenshtein O(n*m) per candidate | Trigram Jaccard via inverted index O(posting) | **Us 8/10** |
| Next-word prediction | N-gram Markov chain | Not needed for product catalog | **Them 7/10** but irrelevant |
| Startup time | Build structures from JSON at runtime | Precomputed CBOR embedded in binary | **Us 10/10** |
| Click learning | Increment trie weights (no decay) | Exponential decay with persistence | **Us 9/10** |
| Overall | 5/10 for product catalog | 8.5/10 | **Us** |

The competitor's Markov chain is overkill for product search. Ghost text completion + match highlighting achieves the same "typeahead feel" without a language model.

---

## Part 1: Performance Fixes for 100K Scale

### Fix A: Top-K Heap Select in BM25 Search

**Problem**: `bm25/bm25.go` lines 238-244 — `slices.SortFunc` over ALL candidates. For a common prefix like "b" in a beer catalog at 100K, this could be 5000+ candidates sorted when only 10 are needed.

**Solution**: Replace full sort with a min-heap of size `maxResults` (10). When scoring candidates, if the heap is full and the new score is higher than the heap minimum, pop the minimum and push the new result. Final results extracted from the heap in descending order.

- Complexity: O(n * log(k)) where k=10 instead of O(n * log(n))
- At 5000 candidates: ~5000 * 3.3 = 16,500 comparisons vs ~5000 * 12.3 = 61,500 comparisons

**Implementation**: Use `container/heap` from stdlib with a min-heap of `SearchResult` ordered by score ascending (so the smallest is on top for eviction).

### Fix B: Pooled Bitset Dedup in BM25 Search

**Problem**: `bm25/bm25.go` line 198 — `seen := make(map[int]struct{})` allocated on every search. Maps with >64 entries trigger heap allocation. For common prefixes at 100K, thousands of map inserts cause GC pressure.

**Solution**: Pool a `[]bool` array sized to product count (same pattern as `index/ngram.go` hitsPool). Use `sync.Pool` with `[]bool` of length N. Set `seen[id] = true` for O(1) dedup, clear touched entries after use.

- Memory per pool slot at 100K: 100KB (1 byte per product)
- Zero heap allocation on the hot path after warmup

### Fix C: Cap Prefix Length to 6 Characters

**Problem**: `bm25/bm25.go` lines 120-134 — stores ALL prefixes of every word. "weihenstephaner" generates 15 prefix entries. At 100K products x avg 3 words x avg 8 chars = 2.4M prefix entries in `prefixPosting`. Bloats memory and CBOR snapshot.

**Solution**: Cap prefix generation at 6 characters. Rationale:
- 1-2 char queries hit the engine's prefix cache (precomputed at boot)
- 3-6 char prefixes cover the "typeahead sweet spot" — "bud", "budw", "budwe", "budwei"
- Beyond 6 chars, the user has typed enough for the term posting list to be precise
- Cuts prefix entries from ~2.4M to ~1.4M at 100K (42% reduction)

**Implementation**: Change the inner loop in `NewIndex` from `for length := 1; length <= len(runes); length++` to `for length := 1; length <= min(len(runes), 6); length++`.

### Fix D: Shrink Hit Array from `[]int16` to `[]uint8`

**Problem**: `index/ngram.go` line 189 — pooled `[]int16` array sized to product count. At 100K, each pool slot is 200KB. Under concurrent load with multiple goroutines, memory pressure grows.

**Solution**: Use `[]uint8` instead. The hit count per product per query is bounded by the number of query trigrams. Queries are capped at 200 bytes (`maxQueryLen` in `main.go`), producing at most ~198 trigrams. After deduplication, a typical query has <30 unique trigrams. `uint8` (max 255) is safe with a saturation guard: `if hits[id] < 255 { hits[id]++ }`.

- Memory per pool slot at 100K: 100KB (down from 200KB)
- No behavior change — hit counts never exceed 255 for any realistic query

### Fix E: Parallel Prefix Cache Build at Startup

**Problem**: `engine/engine.go` lines 160-164 — `buildPrefixCache` iterates every unique 1-2 char prefix sequentially, calling `searchInternal` for each. At 100K products with diverse names, ~700+ prefixes each running the full pipeline. Could take seconds.

**Solution**: Fan out prefix cache computation across `runtime.GOMAXPROCS(0)` goroutines using a bounded worker pool with `sync.WaitGroup`. Each worker processes a batch of prefixes. Results collected into a `sync.Map` or pre-allocated map with mutex.

- Expected speedup: proportional to core count (on the user's 16-core Ryzen 9, ~10-14x)
- Boot time at 100K: from potentially 5-10s down to ~0.5-1s

### Fix F: Sorted Prefix Array for Short-Query Binary Search

**Problem**: `index/ngram.go` lines 244-252 — `prefixSearch` does O(N) linear scan for queries <3 chars. At 100K, this is ~9ms per cache miss.

**Solution**: At index build time, create a sorted array of `(lowercased_name, product_id)` pairs. For short queries, binary search to find the first matching prefix, then scan forward collecting matches until the prefix no longer matches. O(log n + k) where k is the number of matches.

**Implementation**: Add a `sortedNames []nameEntry` field to `Index` where `nameEntry` is `{name string, id int}`, sorted by name. The binary search uses `sort.Search` or `slices.BinarySearchFunc`. Built during `NewIndex` and included in the `Snapshot` for CBOR embedding.

---

## Part 2: Typeahead UX

### Feature A: Match Highlighting

**Problem**: Results show product names as plain text. Users cannot see why a result matched their query.

**Solution**: The engine computes highlight byte offsets during scoring and returns them with each result. The template wraps matched portions in `<mark>` tags.

**Data structures**:

```go
type Highlight struct {
    Start int  // byte offset into Product.Name
    End   int  // byte offset (exclusive)
}

// Added to engine.Result:
type Result struct {
    Product        catalog.Product
    ProductID      int
    Score          float64
    MatchType      MatchType
    Highlights     []Highlight    // matched byte ranges in Product.Name
    HighlightedName template.HTML // pre-rendered HTML with <mark> tags
}
```

**Highlight computation**:
- **BM25 path**: query terms are whole words. Find each matched term's position in the product name using case-insensitive `strings.Index`. Return byte range.
- **Jaccard path**: query is a substring/fuzzy match. Find the best contiguous match of the query in the product name using case-insensitive search. Return byte range.
- **Prefix path**: the query IS the prefix. Highlight bytes 0 through len(query).

**Template rendering**: `buildHighlightedName()` method on Result takes the product name and highlight ranges, produces HTML like:
```html
<mark>Bud</mark>weiser
```

The `HighlightedName` field is `template.HTML` (pre-escaped) so the template can output `{{.HighlightedName}}` without double-escaping.

**Safety**: Only `<mark>` and `</mark>` tags are inserted. Product names are HTML-escaped before insertion. No user-controlled HTML.

### Feature B: Ghost Text Completion

**Problem**: The search input doesn't suggest completions as the user types.

**Solution**: Return the top result's completion suffix as a `data-ghost` attribute on the HTMX response. A thin JS layer renders it as faded text overlaid on the input.

**Backend**: After computing results, if the first result's product name starts with the query (case-insensitive), the ghost text is the remaining suffix. Otherwise, ghost text is empty.

```go
// In handleSearch, after computing results:
ghost := ""
if len(results) > 0 {
    name := results[0].Product.Name
    lowerName := strings.ToLower(name)
    lowerQuery := strings.ToLower(query)
    if strings.HasPrefix(lowerName, lowerQuery) {
        // Use byte length of the matched prefix in the ORIGINAL name
        // Safe because ToLower preserves byte length for ASCII product names
        ghost = name[len(lowerQuery):]
    }
}
```

Note: `strings.ToLower` preserves byte length for ASCII characters, which covers all product names in the catalog. If non-ASCII names are added in future, this should use `utf8.RuneCountInString` for safe offset calculation.

The results template wrapper div gets: `data-ghost="{{.Ghost}}"`.

**Frontend**: The search input is wrapped in a `position: relative` container. A `<span>` with matching font, size, and padding is positioned absolutely behind the input text. On HTMX `afterSwap`, JS reads `data-ghost` from the results container and updates the ghost span.

```
Container (position: relative):
  Ghost span (position: absolute, color: #ccc, pointer-events: none):
    "   weiser"  ← padded to align after user text
  Input (position: relative, background: transparent):
    "bud"        ← user's real text
```

- **Tab key**: accepts the ghost text — fills the input with the full product name, triggers a new HTMX search
- **Any other key**: ghost updates naturally on next HTMX response
- **No ghost if no prefix match**: ghost span is empty, invisible

**No layout shift**: ghost span is absolutely positioned, does not affect document flow.

### Feature C: Keyboard Navigation

**Problem**: Users cannot navigate results with arrow keys.

**Solution**: ~30 lines of vanilla JS in `index.html` handling keydown events on the search input.

**Behavior**:
- `ArrowDown`: add `.active` CSS class to next result row, remove from previous
- `ArrowUp`: move `.active` to previous row
- `Enter` on active row: fire `POST /select` for that product (reuse existing hx-post), fill input with product name, trigger new search
- `Escape`: clear `.active` selection, refocus input
- `afterSwap` event: reset active index to -1 (new results arrived)

**CSS**: `.active` class adds a background highlight (`background: #f0f0f0` or similar) to the result row. Smooth transition.

**No layout shift**: `.active` only changes background color. No margin, padding, or position changes. Results div stays exactly where it is.

**HTMX integration**: The existing `hx-post="/select"` on result rows continues to work for mouse clicks. Keyboard Enter programmatically triggers the same endpoint via `htmx.trigger(activeRow, 'click')`.

---

## Complete Change Scope

| Area | Change | Type |
|---|---|---|
| `bm25/bm25.go` | Top-K heap select, pooled bitset dedup, cap prefix length to 6 | Performance |
| `bm25/bm25_test.go` | Benchmarks for top-K vs full sort, pooled dedup | Performance |
| `index/ngram.go` | `[]uint8` hit array, sorted prefix array + binary search | Performance |
| `index/ngram_test.go` | Binary search fallback test, uint8 saturation test | Performance |
| `engine/engine.go` | Parallel prefix cache, Highlight struct, HighlightedName builder, ghost text | Perf + UX |
| `engine/engine_test.go` | Highlight position tests, ghost text tests | UX |
| `cmd/generate/main.go` | Cap prefix length, pre-sort names for snapshot | Performance |
| `templates/results.html` | `<mark>` highlighting, `data-ghost` attribute | UX |
| `templates/index.html` | Ghost text CSS overlay, keyboard nav JS, Tab-to-accept | UX |
| `bench_test.go` | 100K simulated benchmarks, top-K comparison | Performance |
| `main.go` | Pass ghost text to template data | UX |

### What Does NOT Change

- Bloom filter (already efficient)
- Ranking/popularity system (already good)
- HTMX fragment swap pattern (same `#results` div, same `innerHTML` swap)
- CBOR embedding pipeline (same flow, smaller prefix data)
- Category cache and fallback logic
- HTTP routes and endpoints
- `#results` div position — no layout shift

## Benchmarking Strategy

**Before**: capture benchmarks at current 10K dataset.

**After**: run same benchmarks plus new 100K-simulated benchmarks:
- `BenchmarkBM25Search_100K` — synthetic 100K product index
- `BenchmarkBM25TopK_vs_FullSort` — direct comparison
- `BenchmarkBM25PooledDedup_vs_Map` — allocation comparison
- `BenchmarkPrefixSearch_BinarySearch` — O(log n) vs O(n)
- `BenchmarkPrefixCacheBuild_Parallel` — startup time

Compare with `benchstat` to verify no regression on existing paths and quantify improvements.

## Go Quality Gate

After implementation, run all Go skills across modified files (same gate as BM25 integration).
