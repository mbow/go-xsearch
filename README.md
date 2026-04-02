<div align="center">
  <img src="docs/logo.png" alt="Search Logo" width="200" />
</div>

# go-xsearch

A fast autocomplete search engine built with Go and HTMX. One external
dependency (CBOR for compact binary serialization). No database, no external
services.

![Search Demo](docs/demo.gif)

<!-- Record a video of the search UI in action and save to docs/demo.webm -->

## Why This Exists

You don't need Elasticsearch, Redis, or a vector database to build fast
product search.

A single Go binary — no external services — serves autocomplete for
**10,000+ products in under 3 microseconds**, with typo correction, category
fallback, and popularity ranking.

Built for:

- **Product catalogs** (e-commerce, inventory, menus)
- **Internal tools** where infrastructure is overkill
- **Serverless / edge** where cold start time matters

## What It Does

Type a few characters and get instant results:

- **"bud"** finds Budweiser (prefix match)
- **"budwiser"** still finds Budweiser (typo tolerance)
- **"pod"** finds AirPods Pro (substring match)
- **"beer"** shows the most popular beers (category search)
- **"hoppy"** finds IPAs and Pale Ales (tag search)

Results are ranked by a combination of **how well the query matches** and **how
popular the product is** (based on what users click). Popular items rise to the
top. Stale popularity fades over time.

## How It Works

```
User types "bud"
    |
    v
[Extract trigrams] -- "bud" → ["bud"]
    |
    v
[Bloom filter] -- any trigram in the set? YES → continue
    |
    v
[BM25 + Prefix Boost] -- "bud" is a prefix of "budweiser" → score + boost
    |
    v
[Popularity Ranking] -- blend with click history (exponential decay)
    |
    v
[Highlight + Ghost Text] -- "<mark>Bud</mark>weiser", ghost: "weiser"
    |
    v
[HTMX] -- swaps results into the page, ghost text overlays the input
```

The Bloom filter rejects gibberish queries in ~500 ns before any scoring runs.
When BM25 finds no word-level matches (typos like "budwiser"), the system falls
back to **trigram Jaccard scoring** — overlapping 3-character substrings ranked
by set overlap. This provides typo tolerance without edit distance computation.

## Performance

Benchmarked on an AMD Ryzen 9 5950X with 10,000 products. All data structures
are precomputed and embedded in the binary — zero I/O at startup.

| Query Type                    | Example             |       Time | Allocs | Bytes |
| ----------------------------- | ------------------- | ---------: | -----: | ----: |
| Cached prefix (1-2 chars)     | `"b"`               |  **13 ns** |      0 |     0 |
| Category match                | `"beer"`            |  **18 ns** |      0 |     0 |
| Prefix (3+ chars, BM25)       | `"nik"`             | **240 ns** |      1 |    16 |
| Bloom rejection (gibberish)   | `"xzqwvp"`          | **524 ns** |      1 |    64 |
| Fuzzy / typo (Jaccard)        | `"budwiser"`        | **1.8 µs** |      8 | 1,627 |
| Full BM25 pipeline            | `"budweiser"`       | **2.5 µs** |      8 | 1,643 |
| HTTP response (warm cache)    | `GET /search?q=bud` | **2.2 µs** |     24 | 9,021 |
| HTTP response (cold cache)    | `GET /search?q=bud` |  **38 µs** |    256 |   118K |
| Parallel search (32 threads)  | mixed queries        | **170 ns** |      7 |   902 |

> 1 µs = 0.001 ms. Most queries complete in **under 3 microseconds**. The
> Bloom filter rejects gibberish in 524 ns with a single allocation.

### Search Pipeline

Queries flow through a hybrid pipeline: **BM25 word-level matching** (primary)
with **Jaccard trigram fallback** (for typos). A Bloom filter pre-check
short-circuits gibberish queries before any scoring work begins.

```
Query → Extract trigrams → Bloom pre-check → REJECT → category fallback only
                                           → PASS   → BM25 word match? → YES → score + popularity → top 10
                                                                        → NO  → Jaccard trigram     → top 10
```

Results include match highlighting (`<mark>` tags), ghost text completion, and
keyboard navigation.

### Key Optimisations

- **Bloom-first pipeline** — rejects gibberish before BM25 or Jaccard runs
- **Single trigram extraction** — computed once, passed to Bloom, Jaccard, and
  category lookup (avoids 3x redundant work)
- **Typed min-heap** for top-K — no `container/heap` interface boxing
- **Pooled bitsets** for candidate dedup — zero-alloc on the hot path
- **Stack-allocated buffers** — trigram sort, candidate tracking, and highlight
  arrays stay on the stack for typical queries
- **Slice-indexed ranking** — popularity scores stored in `[]float64` indexed by
  product ID, replacing map lookups
- **Pre-lowered names** — avoids per-result `strings.ToLower`
- **Sort+compact dedup** — replaces `map[string]struct{}` for trigram
  deduplication
- **Parallel prefix cache** — startup scales with CPU cores

### Startup

The product catalog, Bloom filter, n-gram index, and BM25 index are all
**pre-built at compile time** and embedded as gzip-compressed CBOR. Zero file
I/O at startup — the binary serves immediately.

## Tech Stack

| Component         | Technology                                                   |
| ----------------- | ------------------------------------------------------------ |
| Language          | Go 1.26 (standard library)                                   |
| Frontend          | HTMX 2.0.8 (vendored, 50KB)                                  |
| Data format       | CBOR via [fxamacker/cbor](https://github.com/fxamacker/cbor) |
| Database          | None                                                         |
| Cache             | None (in-memory)                                             |
| External services | None                                                         |

The only external dependency is `fxamacker/cbor` for compact binary
serialization of the embedded product data. Everything else — HTTP server, HTML
templates, search algorithms, ranking — is built from scratch using Go's
standard library.

## Quick Start

```bash
# Clone and run
git clone <repo-url>
cd search
go run .

# Open in browser
open http://127.0.0.1:8080

# Bind to a different address
LISTEN_ADDR=0.0.0.0:8080 go run .
```

## Project Structure

```
search/
  main.go                      # Entry point (wiring + ListenAndServe)
  internal/server/             # HTTP handlers, fragment cache, template rendering
  engine/engine.go             # Search orchestrator + highlight computation
  bm25/bm25.go                # BM25 scoring with prefix boost (typed min-heap)
  bloom/bloom.go               # Bloom filter (probabilistic fast rejection)
  index/ngram.go               # N-gram inverted index (trigram Jaccard + binary search)
  ranking/ranking.go           # Popularity ranking (exponential time decay)
  catalog/                     # Product data model + embedded CBOR loader
  cmd/generate/                # Code-generation: JSON -> CBOR with pre-built indexes
  cmd/generate_products/       # Sample data generator (10k beers)
  benchmarks/suite_test.go     # Consolidated benchmark suite
  templates/                   # HTMX HTML templates (highlights, ghost text, keyboard nav)
  static/htmx.min.js          # Vendored HTMX
  data/products.json           # Source product catalog
```

## Updating Product Data

Edit `data/products.json`, then regenerate the embedded binary data:

```bash
go generate ./catalog/
go build .
```

The generator reads JSON, builds the Bloom filter and n-gram index, serializes
everything to gzip-compressed CBOR, and writes it to `catalog/data.cbor`. This
file is embedded in the binary at compile time via `//go:embed`.

## Algorithms

This project implements five search algorithms from scratch:

### BM25 with Prefix Boosting

The primary ranking algorithm. Product names, categories, and tags are tokenized
into words with precomputed IDF (inverse document frequency) values. Queries are
scored using the Okapi BM25 formula. A **prefix bonus** (0.5 x max IDF) is added
when a query term is a prefix of a product name word — this is why "bud" ranks
Budweiser above Funky Buddha.

### Bloom Filter

A probabilistic data structure that answers "is this item definitely NOT in the
set?" in constant time. Uses FNV-1a and DJB2 hash functions with double hashing.
False positives are possible; false negatives are not.

### N-gram Inverted Index (Jaccard Fallback)

Every product name and tag is broken into overlapping 3-character substrings
(trigrams). An inverted index maps each trigram to the products that contain it.
When BM25 finds no word-level matches (typos like "budwiser"), the system falls
back to scoring by **Jaccard similarity** — the ratio of shared trigrams to
total trigrams. This provides typo tolerance without edit distance computation.

### Exponential Decay Ranking

Each time a user clicks a product, the timestamp is recorded. Popularity is
computed as `sum(e^(-0.05 * age_in_days))` — recent clicks count more, old
clicks fade. This is blended with the search relevance score (70% BM25 + 30%
popularity) to produce the final ranking.

### Category Fallback

When a query doesn't match any product well enough, the system matches against
category names instead and returns the most popular products from the
best-matching category.

## Running Benchmarks

```bash
# Quick run (all benchmarks)
make bench

# Record results with commit tracking
make bench-record

# Save as comparison baseline
make bench-save

# Compare against saved baseline (refuses same-commit comparisons)
make bench-compare

# Compare against original pristine baseline (historical progress)
make bench-compare-baseline

# CPU profiling a specific benchmark
go test -bench=BenchmarkComponent_EngineBM25Path -cpuprofile=cpu.prof ./benchmarks/
go tool pprof cpu.prof
```

## Running Tests

```bash
go test ./...
```

## License

MIT
