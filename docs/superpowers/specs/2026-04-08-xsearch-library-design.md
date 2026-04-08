# xsearch Library Extraction Design

**Date:** 2026-04-08
**Status:** Approved

## Goal

Extract the core search functionality from the go-xsearch project into a clean, domain-agnostic Go library (`xsearch/` package). The library handles fuzzy text search over any collection of items with weighted fields. The existing web app becomes a sample/reference implementation showing how to host and use the library.

## Design Decisions

- **Domain-agnostic:** Library knows nothing about products, drinks, or any specific data model. Consumers implement `Searchable`.
- **Field-based with weights:** Consumers define multiple searchable fields with weights. Fields support multiple values (tags, ingredients, variants).
- **String IDs:** IDs are strings to accommodate slugs, UUIDs, or numeric IDs as strings.
- **External scoring via `Scorer` interface:** Library blends relevance with an optional external signal. Consumers implement `Scorer` for popularity, recency, business logic, etc.
- **Bloom filter as optional component:** Exported, configurable, included in snapshots. Consumers enable via `WithBloom()` or use standalone.
- **CBOR snapshots:** Build indices once, serialize, reload fast. CBOR is the only supported format.
- **Immutable indices:** No add/remove after construction. Rebuild to change.
- **No UI concerns in the library:** No `template.HTML`, no HTTP, no caching. The library returns IDs, scores, and highlight spans.

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

### Scorer

```go
// Scorer provides an external score for a searchable item.
// Return 0 to indicate no signal. Scores are normalized internally.
type Scorer interface {
    Score(id string) float64
}
```

### Result

```go
type MatchType int

const (
    MatchDirect   MatchType = iota
    MatchFallback
)

type Highlight struct {
    Start int
    End   int
}

type Result struct {
    ID         string
    Score      float64
    MatchType  MatchType
    Highlights map[string][]Highlight // field name -> highlight spans
}
```

## Engine API

### Construction

```go
// New creates a search engine from a slice of Searchable items.
func New(items []Searchable, opts ...Option) *Engine

// NewFromSnapshot loads a pre-built engine from a CBOR snapshot.
func NewFromSnapshot(data []byte, opts ...Option) *Engine
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
```

### Methods

```go
// Search returns results matching the query, ordered by combined score.
func (e *Engine) Search(query string) []Result

// Snapshot serializes the engine's indices to CBOR for fast reload.
func (e *Engine) Snapshot() ([]byte, error)
```

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
| `index/ngram.go` | `xsearch/ngram.go` | Internals unexported, adapted for Searchable/Field |
| `bm25/bm25.go` | `xsearch/bm25.go` | Internals unexported, adapted for Searchable/Field |
| `bloom/bloom.go` | `xsearch/bloom.go` | Stays exported |
| `engine/engine.go` search logic | `xsearch/xsearch.go` | New API surface wrapping internals |
| Snapshot logic | `xsearch/snapshot.go` | CBOR serialization |

### Adapted in the sample app

- `catalog.Product` gains `SearchID() string` and `SearchFields() []Field` methods
- `ranking.Ranker` gains `Score(id string) float64` method (satisfies `xsearch.Scorer`); ID is now string
- `engine/` package is deleted; `internal/server/` imports `xsearch.Engine` directly
- `internal/server/server.go` uses `xsearch.Result`, handles `template.HTML` highlighting on its own
- `main.go` wires up `xsearch.New()` with options instead of `engine.NewFromEmbedded()`
- `cmd/generate/` uses `xsearch.New()` + `Snapshot()` instead of building indices directly

### Untouched (logic preserved, imports updated)

- `benchmarks/suite_test.go` -- existing benchmarks for regression testing; imports updated from `engine` to `xsearch` but benchmark logic and assertions unchanged
- `internal/server/` templates and static assets
- `data/` directory (popularity JSON, CBOR files)
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
- Scorer integration: verify external scores blend correctly with relevance
- Snapshot round-trip: build engine, snapshot, reload, verify identical results
- Bloom optional behavior: verify search works with and without bloom enabled
- Benchmarks: index construction, search latency, snapshot size

### Sample app tests (unchanged locations)

- `catalog/` tests: verify Product implements Searchable correctly
- `ranking/` tests: verify Ranker implements Scorer correctly
- `internal/server/` tests: HTTP handler behavior, caching, rate limiting
- `benchmarks/suite_test.go`: full-stack regression benchmarks (untouched)

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
engine := xsearch.New(drinks,
    xsearch.WithBloom(100),
    xsearch.WithBM25(1.2, 0.75),
)
results := engine.Search("smoky scotch")
```
