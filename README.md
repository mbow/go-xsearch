<div align="center">
  <img src="docs/logo.png" alt="Search Logo" width="200" />
</div>

# go-xsearch

A fast, zero-dependency** autocomplete search engine built with Go and HTMX. (**
requires CBOR but can be removed for fast start times but not required for all
use cases)

![Search Demo](docs/demo.gif)

<!-- Record a video of the search UI in action and save to docs/demo.webm -->

## Why This Exists

You don't need a vector database, Redis, or Elasticsearch to build a fast
product search even with 100,000 products.

This project proves that a single Go binary — with no external services — can
serve autocomplete results for **10,000+ products in under 3 milliseconds**,
including typo correction, category fallback, and popularity ranking.

It's designed for:

- **Product catalogs** (e-commerce, inventory, menus)
- **Internal tools** where spinning up infrastructure is overkill
- **Serverless / Cloud Functions** where cold start time matters
- **Learning** how search algorithms actually work under the hood

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
[BM25 Index] -- word-level match? "bud" is a prefix of "budweiser" -- YES
    |
    v
[BM25 + Prefix Boost] -- score by term importance, boost prefix matches
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

If the BM25 index finds no word-level matches (typo like "budwiser"), the system
falls back to **trigram Jaccard scoring** — breaking the query into overlapping
3-character substrings and ranking by overlap. A **Bloom filter** fast-rejects
queries that definitely won't match anything.

## Performance

Benchmarked on an AMD Ryzen 9 5950X with 10,000 products. All search data
structures are precomputed and embedded in the binary — zero I/O at startup.

| Query Type                    | Example             |       Time | Allocations |
| ----------------------------- | ------------------- | ---------: | ----------: |
| Cached prefix (1-2 chars)     | `"b"`               |  **13 ns** |           0 |
| Category match                | `"beer"`            |  **19 ns** |           0 |
| Prefix (3+ chars, BM25)       | `"nik"`             | **840 ns** |           4 |
| Bloom rejection (gibberish)   | `"xzqwvp"`          | **1.3 µs** |           4 |
| Prefix boost                  | `"bud"`             | **2.6 µs** |          25 |
| Fuzzy / typo (Jaccard)        | `"budwiser"`        | **2.8 µs** |          17 |
| Full pipeline with popularity | `"budweiser"`       | **3.8 µs** |          18 |
| HTTP response (warm cache)    | `GET /search?q=bud` | **2.3 µs** |          24 |
| HTTP response (cold cache)    | `GET /search?q=bud` |  **40 µs** |         264 |

> 1 µs (microsecond) = 0.001 milliseconds. Most queries complete in **under 4
> microseconds**. The full HTTP round-trip including template rendering is under
> 40 µs on a cache miss.

### Search Pipeline

Queries flow through a hybrid pipeline: **BM25 word-level matching** (primary)
with **Jaccard trigram fallback** (for typos). Results include match highlighting
(`<mark>` tags), ghost text completion, and keyboard navigation.

```
Query → BM25 word match? → YES → score + prefix boost + popularity → top 10
                         → NO  → trigram Jaccard (typo tolerant) → top 10
                                → category fallback if < 3 results
```

### Key Optimisations

- **Typed min-heap** for top-K selection — no `container/heap` interface boxing
- **Pooled bitsets** for candidate dedup — zero-alloc on the hot path
- **Pre-lowered product names** — avoids per-result `strings.ToLower`
- **Sorted slices** replace per-product maps — eliminates 20K map allocations at startup
- **Stack-allocated highlight buffers** — avoids heap escape for ≤ 4 highlights
- **Binary search prefix fallback** — O(log n) for short queries vs O(n) linear scan
- **Parallel prefix cache build** — startup scales with CPU cores

### Startup

The product catalog, Bloom filter, n-gram index, and BM25 index are all
**pre-built at compile time** and embedded in the binary as gzip-compressed CBOR.
There is zero file I/O at startup — the binary is ready to serve immediately.

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
open http://localhost:8080
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
back to scoring by **Jaccard similarity** — the ratio of shared trigrams to total
trigrams. This provides typo tolerance without edit distance computation.

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
