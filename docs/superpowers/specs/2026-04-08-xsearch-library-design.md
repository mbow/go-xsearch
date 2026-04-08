# xsearch Library Extraction Design

**Date:** 2026-04-08
**Status:** Approved (rev 2 — post-review)

## Goal

Extract the core search functionality from the go-xsearch project into a clean, domain-agnostic Go library (`xsearch/` package). The library handles fuzzy text search over any collection of items with weighted fields. The existing web app becomes a sample/reference implementation showing how to host and use the library.

## Design Decisions

- **Domain-agnostic:** Library knows nothing about products, drinks, or any specific data model. Consumers implement `Searchable`.
- **Field-based with weights:** Consumers define multiple searchable fields with weights. Fields support multiple values (tags, ingredients, variants). Weights have a concrete effect on scoring (see Scoring Model).
- **String IDs:** IDs are strings to accommodate slugs, UUIDs, or numeric IDs as strings.
- **External scoring via `Scorer` interface:** Library blends relevance with an optional external signal. Consumers implement `Scorer` for popularity, recency, business logic, etc. Contract is tightened (see Scorer Contract).
- **Bloom filter as optional component:** Exported, configurable, included in snapshots. Consumers enable via `WithBloom()` or use standalone.
- **CBOR snapshots:** Build indices once, serialize, reload fast. CBOR is the only supported format. Snapshots are self-contained and versioned (see Snapshot Contract).
- **Immutable indices:** No add/remove after construction. Rebuild to change.
- **No UI concerns in the library:** No `template.HTML`, no HTTP, no caching of scored results. The library returns IDs, scores, and highlight spans.
- **No internal result caches:** The library caches index structures (posting lists, IDF tables, bloom bits) but not scored results. Scorer is called at search time. Consumers build their own result caching layer if needed.

## Core Interfaces & Types

### Searchable

```go
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
```

### Scorer Contract

```go
// Scorer provides an external score for a searchable item.
// Implementations must return values >= 0.
// Negative values are clamped to 0. NaN and Inf are treated as 0.
// Normalization is per-search: the library collects scores for all candidate
// results, finds the max, and normalizes to [0, 1] before blending with alpha.
// Return 0 to indicate no signal.
//
// Score is called on the hot path for every candidate result.
// Implementations should be O(1) lookups. The library does not cache or
// snapshot scorer results — if the consumer wants caching, implement it
// inside the Scorer.
type Scorer interface {
    Score(id string) float64
}
```

### Result

```go
type MatchType int

const (
    MatchDirect   MatchType = iota // Found via direct n-gram or BM25 match
    MatchFallback                  // Found via fallback group (see WithFallbackField)
)

type Highlight struct {
    Start      int // byte offset (inclusive)
    End        int // byte offset (exclusive)
    ValueIndex int // index into the matched Field.Values slice
}

type Result struct {
    ID         string
    Score      float64
    MatchType  MatchType
    Highlights map[string][]Highlight // field name -> highlight spans
}
```

## Scoring Model

Field weights directly affect how relevance scores are computed. The library runs two scoring paths internally:

### BM25 path (primary — well-formed queries)

Each field is scored independently. The BM25 score for a field is multiplied by the field's weight. The final relevance is the weighted sum across all fields:

```
relevance = sum(bm25(field_i, query) * field_i.Weight)
```

Prefix boosting applies only to the highest-weighted field (the "primary" field — typically the name). This is computed from word prefixes extracted from that field's values only.

### Jaccard/n-gram fallback (fuzzy — handles typos)

Trigrams from all fields contribute to the inverted index. Each field's trigrams are tagged with field identity internally. A document's Jaccard score is the weighted sum of per-field Jaccard similarities:

```
relevance = sum(jaccard(field_i_trigrams, query_trigrams) * field_i.Weight)
```

### Combined score (with external Scorer)

```
final = (1 - alpha) * relevance + alpha * normalized_scorer_value
```

Where `normalized_scorer_value` is the per-search max-normalized output of `Scorer.Score()`.

### Fallback strategy

When direct matches are fewer than a configurable threshold (`minDirectResults`, default 3), and a fallback field is configured via `WithFallbackField(fieldName)`, the engine:

1. Finds the best matching group by comparing query trigrams against the distinct values of the fallback field.
2. Returns top items from that group, scored by external Scorer only (relevance = fixed small value).
3. These results have `MatchType = MatchFallback`.

If `WithFallbackField` is not set, no fallback occurs — only direct matches are returned.

## Engine API

### Construction

```go
// New creates a search engine from a slice of Searchable items.
// Returns an error if any items have duplicate IDs, empty IDs,
// or fields with non-positive weights.
func New(items []Searchable, opts ...Option) (*Engine, error)

// NewFromSnapshot loads a pre-built engine from a CBOR snapshot.
// Returns an error for malformed CBOR, version mismatches, or corrupted data.
// Build-time index options (BM25 params, bloom config) are restored from the
// snapshot. Only WithScorer and WithAlpha can be overridden at load time.
func NewFromSnapshot(data []byte, opts ...Option) (*Engine, error)
```

### Options

```go
// WithScorer sets an external scorer to blend with relevance.
func WithScorer(s Scorer) Option

// WithBloom enables bloom filter pre-rejection.
// bitsPerItem controls false-positive rate (higher = fewer false positives, more memory).
// Set to 0 to disable. Default: disabled.
func WithBloom(bitsPerItem int) Option

// WithBM25 configures BM25 parameters. Defaults: k1=1.2, b=0.75.
func WithBM25(k1, b float64) Option

// WithAlpha sets the blending weight between relevance and external score.
// 0.0 = relevance only, 1.0 = external score only. Default: 0.6.
func WithAlpha(alpha float64) Option

// WithLimit sets the maximum number of results returned per search.
// Default: 10. Clamped to [2, 100].
func WithLimit(n int) Option

// WithFallbackField sets the field name used for group-based fallback.
// When direct matches are sparse, the engine finds the best matching group
// from distinct values of this field and returns top items from that group.
// If not set, no fallback occurs.
func WithFallbackField(fieldName string) Option
```

### Methods

```go
// Search returns results matching the query, ordered by combined score.
func (e *Engine) Search(query string) []Result

// Snapshot serializes the engine's indices to CBOR for fast reload.
func (e *Engine) Snapshot() ([]byte, error)
```

## Snapshot Contract

Snapshots are **self-contained** CBOR blobs. They embed:
- All item IDs and field text (needed for highlighting and fallback grouping)
- N-gram inverted index structures
- BM25 index structures (term frequencies, IDF values, posting lists, prefix data)
- Bloom filter bitset (if enabled at build time)
- Build-time configuration (BM25 k1/b, bloom bitsPerItem, fallback field name, limit)
- Version header: magic bytes (`XSRC`) + uint8 version number

`NewFromSnapshot` rejects snapshots with unknown magic bytes or unsupported versions.

Build-time options (BM25 params, bloom config, fallback field) are stored in and restored from the snapshot. Only runtime options (`WithScorer`, `WithAlpha`) can be overridden at load time via `NewFromSnapshot` options. Passing build-time options to `NewFromSnapshot` is an error.

## Bloom Filter

Exported as a standalone, optional component:

```go
// Bloom is a space-efficient probabilistic filter for fast rejection.
type Bloom struct { /* unexported fields */ }

// NewBloom creates a bloom filter sized for n items.
// bitsPerItem controls accuracy -- higher means fewer false positives.
// Typical values: 10 (1% FP rate), 100 (near-zero FP rate).
func NewBloom(n int, bitsPerItem int) *Bloom

// Add inserts a key into the filter.
func (b *Bloom) Add(key string)

// MayContain returns true if the key might be in the set.
// False positives are possible; false negatives are not.
func (b *Bloom) MayContain(key string) bool
```

When `WithBloom(bitsPerItem)` is set, the engine automatically populates the bloom filter during construction and uses it to skip expensive index lookups. The bloom filter state is included in CBOR snapshots.

Consumers can also use `Bloom` directly, independent of the engine.

## Package Structure

```
go-xsearch/
├── xsearch/                    <- THE LIBRARY (public API)
│   ├── xsearch.go              <- Engine, Searchable, Scorer, Field, Option, New()
│   ├── result.go               <- Result, MatchType, Highlight
│   ├── bloom.go                <- Bloom filter (exported, optional component)
│   ├── ngram.go                <- n-gram index (unexported internals)
│   ├── bm25.go                 <- BM25 scoring (unexported internals)
│   ├── snapshot.go             <- CBOR serialize/deserialize
│   └── xsearch_test.go         <- library tests
│
├── catalog/                    <- SAMPLE APP: data model
│   ├── catalog.go              <- Product implements xsearch.Searchable
│   └── embed.go                <- embedded product data
│
├── ranking/                    <- SAMPLE APP: implements xsearch.Scorer
│   └── ranking.go
│
├── internal/
│   └── server/                 <- SAMPLE APP: HTTP layer, templates, caching
│       └── server.go           <- imports xsearch, catalog, ranking
│
├── cmd/
│   └── generate/               <- SAMPLE APP: builds CBOR snapshots
│
├── main.go                     <- SAMPLE APP: wires everything together
└── go.mod
```

**Dependency rule:** `xsearch/` imports nothing from the rest of the project. The sample app imports `xsearch`. One-way dependency.

## Migration Path

### Moves into `xsearch/`

| Source | Destination | Notes |
|--------|-------------|-------|
| `index/ngram.go` | `xsearch/ngram.go` | Internals unexported, adapted for Searchable/Field; per-field trigram tracking |
| `bm25/bm25.go` | `xsearch/bm25.go` | Internals unexported, adapted for per-field BM25 scoring |
| `bloom/bloom.go` | `xsearch/bloom.go` | Stays exported |
| `engine/engine.go` search logic | `xsearch/xsearch.go` | New API surface; prefix/category caches removed (consumers cache externally) |
| Snapshot logic | `xsearch/snapshot.go` | Self-contained CBOR with version header |

### Adapted in the sample app

**`catalog/`:**
- `Product` gains `SearchID() string` and `SearchFields() []Field` methods
- `SearchID()` returns `strconv.Itoa(index)` or a string-based ID scheme

**`ranking/`:**
- Selection map changes from `map[int][]time.Time` to `map[string][]time.Time`
- `RecordSelection(productID int)` becomes `RecordSelection(id string)`
- `Score(id string) float64` method added (satisfies `xsearch.Scorer`)
- `Save()`/`Load()` persistence format changes — existing `popularity.json` files are incompatible (fresh start or one-time migration)
- `ScoreView` adapted for string IDs

**`internal/server/`:**
- Imports `xsearch.Engine` directly instead of `engine.Engine`
- Uses `xsearch.Result` — no more `result.Product` or `result.HighlightedName`
- Server builds `template.HTML` highlighting from `xsearch.Result.Highlights` spans + catalog lookup by string ID
- `/select` handler: removes `strconv.Atoi` validation, validates string ID against a known ID set (e.g., `catalog.GetByID(id)`)
- `writeResultItem` uses string ID in `hx-vals`

**`main.go`:**
- Wires up `xsearch.New()` with options instead of `engine.NewFromEmbedded()`
- Passes `xsearch.WithFallbackField("category")` to preserve current behavior
- Passes `xsearch.WithBloom(100)` to enable bloom pre-filtering

**`cmd/generate/`:**
- Uses `xsearch.New()` + `Snapshot()` to produce self-contained CBOR blob
- Single output file instead of separate bloom/index/bm25 blobs

### Untouched (logic preserved, imports updated)

- `benchmarks/suite_test.go` -- existing benchmarks for regression testing; imports updated from `engine` to `xsearch`, int IDs adapted to string IDs, but benchmark logic and assertions unchanged
- `internal/server/` templates and static assets
- `data/` directory structure preserved; `popularity.json` format changes (string-keyed)
- `bench-latest.txt`, `bench-prev.txt` -- regression baselines preserved

### Deleted after migration

- `bloom/` (moved to `xsearch/`)
- `index/` (moved to `xsearch/`)
- `bm25/` (moved to `xsearch/`)
- `engine/` (replaced by `xsearch/`)

## Testing Strategy

### Library tests (`xsearch/`)

- Unit tests for each internal component (n-gram, BM25, bloom)
- Integration tests: construct an Engine with test Searchable items, verify search results
- Scorer integration: verify external scores blend correctly with relevance; verify clamping of negative/NaN/Inf values
- Snapshot round-trip: build engine, snapshot, reload, verify identical results; verify version rejection for bad snapshots
- Bloom optional behavior: verify search works with and without bloom enabled
- Fallback field: verify fallback grouping works, verify no fallback when not configured
- Limit: verify WithLimit clamping and result count
- Validation: verify New() rejects duplicate IDs, empty IDs, non-positive weights
- Multi-value highlights: verify ValueIndex correctness for fields with multiple values
- Benchmarks: index construction, search latency, snapshot size

### Sample app tests (unchanged locations)

- `catalog/` tests: verify Product implements Searchable correctly
- `ranking/` tests: verify Ranker implements Scorer correctly; adapted for string IDs
- `internal/server/` tests: HTTP handler behavior, caching, rate limiting; adapted for string IDs in /select
- `benchmarks/suite_test.go`: full-stack regression benchmarks (logic untouched, IDs adapted)

### Test helpers

```go
// In xsearch_test.go
type testItem struct {
    id     string
    fields []Field
}

func (t testItem) SearchID() string      { return t.id }
func (t testItem) SearchFields() []Field { return t.fields }
```

Library tests use only library types. Sample app tests exercise the full stack. No circular dependencies.

## Example: Drink Menu Consumer

```go
type Drink struct {
    ID          string
    Name        string
    Canonical   string
    Category    string
    Ingredients []string
    Variants    []string
    Tags        map[string][]string
}

func (d Drink) SearchID() string { return d.ID }

func (d Drink) SearchFields() []xsearch.Field {
    fields := []xsearch.Field{
        {Name: "name", Values: []string{d.Name}, Weight: 1.0},
        {Name: "canonical", Values: []string{d.Canonical}, Weight: 0.8},
        {Name: "category", Values: []string{d.Category}, Weight: 0.5},
    }
    if len(d.Ingredients) > 0 {
        fields = append(fields, xsearch.Field{
            Name: "ingredients", Values: d.Ingredients, Weight: 0.4,
        })
    }
    if len(d.Variants) > 0 {
        fields = append(fields, xsearch.Field{
            Name: "variants", Values: d.Variants, Weight: 0.3,
        })
    }
    for key, vals := range d.Tags {
        fields = append(fields, xsearch.Field{
            Name: key, Values: vals, Weight: 0.3,
        })
    }
    return fields
}

// Usage
engine, err := xsearch.New(drinks,
    xsearch.WithBloom(100),
    xsearch.WithBM25(1.2, 0.75),
    xsearch.WithFallbackField("category"),
    xsearch.WithLimit(20),
)
if err != nil {
    log.Fatal(err)
}
results := engine.Search("smoky scotch")
```
