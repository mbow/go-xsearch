# Autocomplete Search Engine — Design Spec

## Overview

A Go stdlib-only backend for a product catalog autocomplete search box. The frontend is a single HTML page using HTMX for dynamic, keystroke-driven search. The system provides prefix, substring, and fuzzy matching with popularity-based ranking that learns from user selections.

This is a learning exercise to understand how search algorithms work from the ground up.

## Requirements

- **Fixed product catalog** (~hundreds of items) with category tags, updated monthly
- **Match types:** prefix, substring, and fuzzy (typo-tolerant)
- **Ranking:** frequency-based popularity with recency decay
- **Category fallback:** when no direct match found, show popular items from the best-matching category
- **Frontend:** single text input with HTMX, server renders HTML fragments
- **Persistence:** popularity data snapshotted to disk periodically, loaded on startup
- **Dependencies:** Go standard library only (HTMX vendored as a static JS file)
- **TDD:** tests written before implementation

## Architecture

```
HTTP Layer (net/http + html/template + HTMX)
       |
   Search Engine (orchestrates query flow)
       |
  +----+----------+
  |    |          |
Bloom  N-gram    Ranking
Filter Index     Engine
       |
   Category
   Fallback
```

### Request Flow

1. HTMX sends `GET /search?q=sh` with 250ms debounce on keyup
2. HTTP handler passes query to the search engine
3. Search engine normalizes query (lowercase, trim)
4. Extract trigrams from query
5. Check Bloom filter for each trigram
6. If Bloom says "definitely not" for ALL trigrams, return empty results
7. If any trigram passes Bloom, query the n-gram inverted index
8. Get candidate products with Jaccard similarity scores
9. If fewer than 3 candidates score above a Jaccard threshold of 0.2, trigger category fallback
10. Ranking engine combines relevance score with popularity score
11. Server renders HTML fragment via `html/template`
12. HTMX swaps fragment into the page

### Selection Flow

1. User clicks a result, HTMX sends `POST /select` with product ID
2. Backend records selection timestamp for that product
3. Every 60 seconds, popularity data is snapshotted to a JSON file

## Component Designs

### Bloom Filter (`bloom/bloom.go`)

A probabilistic data structure for fast rejection of queries with no possible matches.

**Structure:**
- Bit array: `[]uint64` slice, individual bits set/checked via bitwise operations
- Hash functions: two independent hashes (FNV-1a and DJB2), combined via double hashing to derive `k` hash values
- Parameters: ~20,000 bits (~2.5KB) with k=3 hash functions for ~2,000 unique trigrams, yielding <1% false positive rate

**Operations:**
- `Add(trigram string)` — hash the trigram k ways, set those bit positions
- `MayContain(trigram string) bool` — hash the trigram k ways, return true only if all bits are set

**Lifecycle:**
- Built from scratch on startup and on catalog reload
- Contains every trigram from every product name and every category name

**Learning concepts:** probabilistic data structures, false positives vs false negatives, bit manipulation, double hashing technique.

### N-gram Inverted Index (`index/ngram.go`)

Maps character trigrams to the products that contain them. Handles prefix, substring, and fuzzy matching through a single mechanism.

**Tokenization:**
- Normalize: lowercase, trim whitespace
- Extract overlapping trigrams: "shoes" -> ["sho", "hoe", "oes"]
- Category names are also tokenized and indexed

**Index structure:**
- `map[string][]int` — trigram string to slice of product IDs
- Product store: `[]Product` slice where index position is the product ID

**Query algorithm:**
1. Extract trigrams from query
2. For each trigram, look up the posting list (slice of product IDs)
3. Take the union of all posting lists
4. Score each candidate by Jaccard similarity: `|query_trigrams intersect product_trigrams| / |query_trigrams union product_trigrams|`

**Short query fallback (1-2 characters):**
- Trigrams require 3+ characters
- For shorter queries, linear prefix scan over all product names
- With hundreds of items this is instant

**Why union not intersection:**
- Union provides fuzzy tolerance: a typo corrupts 1-2 trigrams but the remaining trigrams still match
- Intersection would require ALL trigrams to match, breaking fuzzy search

**Learning concepts:** inverted indexes, n-gram tokenization, Jaccard similarity, union vs intersection trade-offs.

### Ranking Engine (`ranking/ranking.go`)

Combines n-gram relevance with popularity to rank results.

**Popularity Score — exponential decay:**
```
score = sum( e^(-lambda * age_in_days) )
```
- Each user selection contributes to the sum
- Older selections contribute less via exponential decay
- Lambda = 0.05 (tunable): a selection from 14 days ago is worth ~half of today's
- Handles both frequency (more selections = higher sum) and recency (recent ones count more)

**Relevance Score:**
- Jaccard similarity from the n-gram index (0.0 to 1.0)

**Popularity normalization:**
- Raw popularity scores are unbounded (more selections = higher sum)
- Normalize to 0.0-1.0 range by dividing by the maximum popularity score across all products
- If no selections exist yet, all popularity scores are 0.0

**Combined final score:**
```
final = (alpha * relevance) + ((1 - alpha) * normalized_popularity)
```
- Alpha = 0.6 (tunable): relevance matters slightly more than popularity
- Both inputs are in the 0.0-1.0 range, so alpha works as expected
- For category fallback results, relevance is set to a low fixed value (0.1)

**Persistence:**
- In-memory: `map[int][]time.Time` — product ID to slice of selection timestamps
- Snapshot: every 60 seconds, marshal to `data/popularity.json`
- Startup: load from `data/popularity.json`
- Pruning: discard timestamps older than 90 days

**Learning concepts:** exponential decay functions, score fusion, tuning constants.

### Category Fallback

When the n-gram index returns zero or very low-scoring results:

1. Match query trigrams against category name trigrams
2. Find the best-matching category
3. Return the most popular products in that category
4. These results are marked as "fallback" so the UI can display them in a separate "Related products" section

This provides the "searched for budwiser, showing other beers" behavior without any ML model.

### Search Engine (`engine/engine.go`)

Orchestrator that ties all components together:

1. Receives a query string
2. Normalizes it
3. Routes through Bloom filter -> n-gram index -> category fallback -> ranking
4. Returns a ranked, scored result set with match type metadata (direct vs fallback)

### HTTP Layer (`main.go`)

**Endpoints:**
- `GET /` — serves the full HTML page
- `GET /search?q=...` — returns HTML fragment of ranked results
- `POST /select` — records a product selection

**Templates:**
- `templates/index.html` — full page with HTMX input box
- `templates/results.html` — fragment with result list, separate sections for direct matches and category fallback

### HTMX Frontend

Single HTML page:
- One `<input>` with `hx-get="/search"` `hx-trigger="keyup changed delay:250ms"` `hx-target="#results"`
- Results div swapped on each response
- Selection sends `POST /select` via `hx-post`
- HTMX loaded from vendored `static/htmx.min.js`

## Project Layout

```
search/
  main.go                  # HTTP server, routes, startup
  bloom/
    bloom.go               # Bloom filter implementation
    bloom_test.go
  index/
    ngram.go               # N-gram inverted index + Jaccard scoring
    ngram_test.go
  ranking/
    ranking.go             # Popularity tracking, decay, score fusion
    ranking_test.go
  engine/
    engine.go              # Search orchestrator
    engine_test.go
  catalog/
    catalog.go             # Product loading, category data
    catalog_test.go
  static/
    htmx.min.js            # Vendored HTMX
  templates/
    index.html             # Full page
    results.html           # Search results fragment
  data/
    products.json          # Product catalog (name + category)
    popularity.json        # Popularity snapshots
```

## Data Format

### products.json
```json
[
  {"name": "Budweiser", "category": "beer"},
  {"name": "Miller Lite", "category": "beer"},
  {"name": "Nike Air Max", "category": "shoes"}
]
```

### popularity.json
```json
{
  "0": ["2026-03-29T10:00:00Z", "2026-03-28T15:30:00Z"],
  "2": ["2026-03-29T09:00:00Z"]
}
```

## Testing Strategy

All components built via TDD — tests written before implementation:

- **bloom:** test Add/MayContain, verify no false negatives, measure false positive rate
- **index:** test trigram extraction, index building, Jaccard scoring, short query fallback, category matching
- **ranking:** test decay calculation, score fusion, persistence round-trip, timestamp pruning
- **engine:** integration tests for full query flow, Bloom rejection path, category fallback path
- **HTTP:** test handler responses, template rendering, HTMX fragment format
