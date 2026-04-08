# xsearch Library Design Review Findings

**Date:** 2026-04-08

An in-depth review of the `2026-04-08-xsearch-library-design.md` spec reveals a few fundamental gaps, primarily around preserving the performance characteristics achieved at the 100K scale and translating domain-specific features (like category fallback) into a domain-agnostic generic model. 

Here are the critical missing pieces that need to be incorporated into the design before implementation:

## 1. Hot Path Performance: Scorer API & String IDs

**Gap:** The proposed `Scorer` interface uses `Score(id string) float64`. Calling this in the inner BM25/Jaccard scoring loops (which evaluates up to thousands of candidates per query) forces passing strings and requires the consumer to perform a map lookup `string -> popularity`. This will severely degrade the allocation and latency improvements recently made at the 100K scale.
**Fix:** 
- The engine must internally map the `items []Searchable` slice order to a dense index `[0, N-1]`.
- The interface should be `type Scorer interface { Score(docIndex int) float64 }`. This allows the consumer to maintain a contiguous `[]float64` of popularity scores in the same order as the slice, yielding lock-free O(1) array access.

## 2. Lock-free Scoring: Scorer as a Search Option

**Gap:** Providing `WithScorer(s Scorer)` as an `Engine` construction option implies a global scorer. Since HTTP search requests are concurrent, a global `Scorer` forces the consumer to use an `RLock` on every candidate evaluation. The `go-xsearch` project currently solves this by yielding a lightweight `ScoreView` snapshot per request.
**Fix:** Instead of `EngineOption`, make the Scorer a per-request `SearchOption`:
```go
// Allows passing ranker.ScoreView() for lock-free scoring
func (e *Engine) Search(query string, opts ...SearchOption) []Result
```

## 3. NewFromSnapshot requires items

**Gap:** The design specifies `func NewFromSnapshot(data []byte, opts ...Option) *Engine`. Without the `[]Searchable` items loaded in memory, the library cannot retrieve the textual data needed to compute `Highlights map[string][]Highlight` for the top results. Storing all text directly inside the CBOR snapshot would massively bloat memory and binary size.
**Fix:** The signature must be updated to require the items:
```go
func NewFromSnapshot(data []byte, items []Searchable, opts ...Option) *Engine
```

## 4. Multi-Field Behavior: Prefix Boosting

**Gap:** `go-xsearch` intentionally caps BM25 prefix boosting to `Product.Name` to prevent memory bloat. In a generic system, generating word prefixes for *all* `SearchFields()` (which might include multi-paragraph descriptions or long ingredient lists) would explode memory.
**Fix:** Add a `PrefixBoost bool` field to `xsearch.Field`. Only fields marked as such should generate word prefixes and populate the 1-2 character startup cache.

## 5. Multi-Field Behavior: Categorical Fallback

**Gap:** The design introduces `MatchType: MatchFallback`, but doesn't define how a generic engine knows which field is "categorical". Currently, `go-xsearch` specifically bypasses term scoring and relies on exactly matching `Product.Category`. 
**Fix:** Introduce a `Fallback bool` capability on `xsearch.Field` (or a `FieldType` enum). This explicitly signals to the library which fields should build the fallback indices and category caching layers.

## 6. BM25 Field Weights (BM25F)

**Gap:** The design adds `Weight float64` per field, but doesn't specify how BM25 handles it. Applying weights at the end of scoring breaks BM25 term saturation math.
**Fix:** Explicitly define the use of the **BM25F** formula: field weights must be multiplied by the term frequency (`tf`) *prior* to saturation, so that a match in a heavily weighted field logically counts as "more occurrences".
