<div align="center">
  <img src="docs/logo.png" alt="Search Logo" width="200" />
</div>

# go-xsearch

A fast, zero-dependency** autocomplete search engine built with Go and HTMX. (**
requires CBOR but can be removed for fast start times but not required for all
use cases)

<video src="docs/demo.webm" autoplay loop muted playsinline width="100%"></video>

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
[Bloom Filter] -- "do any products contain these trigrams?" -- YES
    |
    v
[N-gram Index] -- break "bud" into trigrams, find matching products
    |
    v
[Jaccard Scoring] -- rank by trigram overlap (fuzzy matching)
    |
    v
[Popularity Ranking] -- boost items users click on most
    |
    v
[HTML Fragment] -- server renders just the results list
    |
    v
[HTMX] -- swaps the results into the page, no JavaScript framework needed
```

If the Bloom filter says "definitely no matches," the search returns empty in
**nanoseconds** without touching the index. This is how the system stays fast
even at scale.

## Performance

Benchmarked on an AMD Ryzen 9 5950X with **10,035 products** (10,000 beers +
spirits, shoes, phones, headphones, sodas):

| Query Type                    | Example             |       Time | Allocations |
| ----------------------------- | ------------------- | ---------: | ----------: |
| Cached prefix (1-2 chars)     | `"b"`               |  **13 ns** |           0 |
| Category match                | `"beer"`            |  **16 ns** |           0 |
| Prefix (3+ chars)             | `"nik"`             | **1.1 us** |           8 |
| Bloom rejection (gibberish)   | `"xzqwvp"`          | **1.4 us** |           4 |
| Fuzzy / typo                  | `"budwiser"`        | **2.8 us** |          13 |
| Full pipeline with popularity | `"budweiser"`       | **5.7 us** |          18 |
| HTTP response (warm cache)    | `GET /search?q=bud` | **2.4 us** |          24 |
| HTTP response (cold cache)    | `GET /search?q=bud` |  **45 us** |         257 |

> 1 us (microsecond) = 0.001 milliseconds. Most queries complete in **under 6
> microseconds**.

### Startup

The product catalog, Bloom filter, and n-gram index are all **pre-built at
compile time** and embedded in the binary as gzip-compressed CBOR. There is zero
file I/O at startup — the binary is ready to serve immediately.

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
  main.go                 # HTTP server, routes, template rendering
  bloom/bloom.go          # Bloom filter (probabilistic fast rejection)
  index/ngram.go          # N-gram inverted index (trigram search + Jaccard scoring)
  ranking/ranking.go      # Popularity ranking (exponential time decay)
  engine/engine.go        # Search orchestrator (wires everything together)
  catalog/                # Product data model + embedded CBOR loader
  cmd/generate/           # Code-generation: JSON -> CBOR with pre-built index
  cmd/generate_products/  # Sample data generator (10k beers)
  templates/              # HTMX HTML templates
  static/htmx.min.js     # Vendored HTMX
  data/products.json      # Source product catalog
  bench_test.go           # Performance benchmarks
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

This project implements four search algorithms from scratch:

### Bloom Filter

A probabilistic data structure that answers "is this item definitely NOT in the
set?" in constant time. Uses FNV-1a and DJB2 hash functions with double hashing.
False positives are possible; false negatives are not.

### N-gram Inverted Index

Every product name and tag is broken into overlapping 3-character substrings
(trigrams). An inverted index maps each trigram to the products that contain it.
Queries are tokenized the same way, and candidates are scored by **Jaccard
similarity** — the ratio of shared trigrams to total trigrams.

### Exponential Decay Ranking

Each time a user clicks a product, the timestamp is recorded. Popularity is
computed as `sum(e^(-0.05 * age_in_days))` — recent clicks count more, old
clicks fade. This is combined with the search relevance score to produce the
final ranking.

### Category Fallback

When a query doesn't match any product well enough, the system matches against
category names instead and returns the most popular products from the
best-matching category. This is how "budwiser" (not in catalog) can still show
you other beers.

## Running Benchmarks

```bash
# All benchmarks
go test -bench=. -benchmem .

# Specific benchmark
go test -bench=BenchmarkEngineSearch_Fuzzy -benchmem .

# With CPU profiling
go test -bench=BenchmarkEngineSearch_Fuzzy -cpuprofile=cpu.prof .
go tool pprof cpu.prof
```

## Running Tests

```bash
go test ./...
```

## License

MIT
