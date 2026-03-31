# BM25 Scoring Layer Design

**Date**: 2026-03-31
**Problem**: Searching "bud" returns all "Funky Buddha" variants but not "Budweiser" first. The current Jaccard similarity over trigrams treats all substring matches equally with no concept of term importance or prefix position.
**Solution**: Add a BM25 scoring layer with prefix boosting as the primary ranking path, falling back to Jaccard for fuzzy/typo queries.

## Architecture

### New `bm25/` Package

A new package mirroring the existing `bloom/` and `index/` pattern. All values precomputed at build time, serialized to CBOR, embedded in the binary.

#### Data Structures

```go
type Index struct {
    idf          map[string]float64          // precomputed IDF per term
    termFreqs    []map[string]int            // product ID -> term -> frequency
    docLens      []int                       // product ID -> word count
    avgDocLen    float64                     // mean document length
    wordPrefixes []map[string]struct{}       // product ID -> set of word prefixes
    posting      map[string][]int            // term -> product IDs
    k1           float64                     // TF saturation (1.2)
    b            float64                     // length normalization (0.75)
}
```

#### Tokenization

Split product names on whitespace and punctuation into lowercase words. Category and tags indexed as additional terms with the same tokenizer.

#### Precomputation (build time)

- IDF for every unique term: `log((N - df + 0.5) / (df + 0.5) + 1)`
- Term frequency maps per product
- Document lengths (word count per product)
- Average document length across corpus
- Word prefix sets per product (all prefixes of every word, from length 1 up to full word length, e.g. "budweiser" stores {"b","bu","bud","budw","budwe","budwei","budweis","budweise","budweiser"})
- Word-level inverted posting lists

#### Snapshot

```go
type Snapshot struct {
    IDF          map[string]float64   `cbor:"idf"`
    TermFreqs    []map[string]int     `cbor:"term_freqs"`
    DocLens      []int                `cbor:"doc_lens"`
    AvgDocLen    float64              `cbor:"avg_doc_len"`
    WordPrefixes [][]string           `cbor:"word_prefixes"`
    Posting      map[string][]int     `cbor:"posting"`
    K1           float64              `cbor:"k1"`
    B            float64              `cbor:"b"`
}
```

Serialized to CBOR alongside bloom and index snapshots in `catalog/data.cbor`.

## BM25 Scoring Formula

At query time, given query tokens `[q1, q2, ...]`, score a candidate product:

```
BM25(product, query) = Sum over qi of:
    IDF(qi) * (tf(qi, product) * (k1 + 1)) / (tf(qi, product) + k1 * (1 - b + b * docLen / avgDocLen))
```

All values are precomputed lookups. Query-time cost is multiplication and division per term per candidate.

**Parameters**: `k1 = 1.2`, `b = 0.75`.

### Prefix Boost

If any query token is a prefix of any word in the product name, add a flat bonus:

```
prefix_bonus = 0.5 * max(IDF across all query terms)
```

This pushes "Budweiser" above "Funky Buddha" for query "bud" because "bud" is a prefix of the word "budweiser" but not a prefix of any word in "funky buddha" (it is a substring of "buddha", not a word prefix).

## Engine Integration: Hybrid Pipeline

### Current Flow

```
query -> Bloom -> trigram index (Jaccard) -> popularity blend -> results
```

### New Flow

```
query -> tokenize into words
       |-- Word matches found? -> BM25 scoring + prefix boost -> popularity blend
       |-- No word matches?    -> trigram index (Jaccard fallback) -> popularity blend
       |-- Category fallback (unchanged)
```

### Pipeline Steps

1. Tokenize query into words
2. Look up BM25 word-level posting lists for candidates
3. If candidates found: score with BM25 + prefix boost, blend with popularity
4. If no candidates (typo/partial like "budwiser"): fall back to existing trigram Jaccard path
5. Category fallback unchanged

### Blend Formulas

- **BM25 path**: `0.5 * normalized_bm25 + 0.2 * prefix_boost + 0.3 * popularity`
- **Jaccard fallback path**: `0.6 * jaccard + 0.4 * popularity` (unchanged)

BM25 scores normalized by dividing by max BM25 score in the candidate set, same pattern as existing popularity normalization.

### Structural Changes

- `Engine` struct: add `bm25 *bm25.Index` field
- `NewFromEmbedded`: add `bm25Raw []byte` parameter, deserialize BM25 snapshot
- `searchInternal`: implement two-path logic (BM25 primary, Jaccard fallback)
- `cmd/generate/main.go`: build BM25 index, add `BM25Snap` to `Payload`
- `catalog/embed.go`: deserialize BM25 snapshot alongside bloom and index

## Benchmarking Strategy: Before/After with Regression Guard

### 1. Baseline Snapshot (before)

Capture current benchmarks before any code changes:

```bash
go test -bench=. -benchmem -count=6 ./... | tee docs/benchmarks/baseline.txt
```

Committed as `docs/benchmarks/baseline.txt`.

### 2. New BM25 Benchmarks (`bm25/bm25_test.go`)

- `BenchmarkBM25Score` -- score a single product against a query (sub-microsecond target)
- `BenchmarkBM25Search` -- find and score all candidates for a query
- `BenchmarkBM25PrefixBoost` -- prefix detection overhead
- `BenchmarkBM25FromSnapshot` -- deserialization time (boot cost)

### 3. Updated Engine Benchmarks (`bench_test.go`)

New cases added to existing benchmark suite:

- `BenchmarkEngineSearch_BM25Path` -- query hitting BM25 word-match path ("budweiser")
- `BenchmarkEngineSearch_JaccardFallback` -- query falling back to Jaccard ("budwiser" typo)
- `BenchmarkEngineSearch_PrefixBoost` -- short query triggering prefix boost ("bud")

### 4. After Snapshot + Comparison

```bash
go test -bench=. -benchmem -count=6 ./... | tee docs/benchmarks/after-bm25.txt
benchstat docs/benchmarks/baseline.txt docs/benchmarks/after-bm25.txt
```

Committed as `docs/benchmarks/after-bm25.txt` with benchstat comparison output.

### 5. Ranking Quality Test

New test in `engine/engine_test.go` asserting that query "bud" returns "Budweiser" before any "Funky Buddha" result. This is the concrete regression test for the motivating bug.

## Complete Change Scope

| Area | Change |
|---|---|
| New `bm25/` package | `bm25.go` (Index, Snapshot, Score, Search, prefix boost), `bm25_test.go` (unit tests + benchmarks) |
| `index/ngram.go` | No changes -- trigram index stays as-is for Jaccard fallback |
| `engine/engine.go` | Add `bm25.Index` field, hybrid pipeline (BM25 primary, Jaccard fallback), new blend weights |
| `engine/engine_test.go` | Ranking quality test: "bud" returns Budweiser before Funky Buddha |
| `cmd/generate/main.go` | Build BM25 index, add `BM25Snap` to Payload, serialize to CBOR |
| `catalog/embed.go` | Deserialize BM25 snapshot alongside bloom + index |
| `bench_test.go` | New BM25-path benchmarks, prefix boost benchmark |
| `docs/benchmarks/` | `baseline.txt` (before), `after-bm25.txt` (after), benchstat comparison |

### What Does NOT Change

- Bloom filter (still used for fast rejection before Jaccard fallback)
- Popularity ranking (still blended in)
- Prefix cache (1-2 char queries, rebuilt with new pipeline)
- Category cache and fallback logic
- HTMX frontend, HTTP handlers
- Product data model

## Go Quality Gate

After implementation is complete and all tests pass, run all relevant Go skills across new and modified files:

| Skill | Purpose |
|---|---|
| `golang-modernize` | Latest Go 1.26 idioms, stdlib improvements |
| `golang-code-style` | Formatting, conventions, naming |
| `golang-naming` | Package/function/variable naming conventions |
| `golang-safety` | Nil-prone types, panics, silent corruption |
| `golang-performance` | Allocation reduction, hot-path optimization |
| `golang-error-handling` | Wrapping, sentinel errors, idiomatic patterns |
| `golang-data-structures` | Slices, maps, preallocation, capacity hints |
| `golang-testing` | Table-driven tests, testify, benchmarks |
| `golang-benchmark` | Benchmark writing, pprof, benchstat |
| `golang-concurrency` | sync.Pool usage, RWMutex patterns |
| `golang-structs-interfaces` | Struct design, receiver types |

This is the final quality gate before the closing commit.
