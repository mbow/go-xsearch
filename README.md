<div align="center">
  <img src="docs/logo.png" alt="go-xsearch" width="180" />

# go-xsearch

**Autocomplete search engine. Single binary. Zero infrastructure.**

[![CI](https://github.com/mbow/go-xsearch/actions/workflows/ci.yml/badge.svg)](https://github.com/mbow/go-xsearch/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/mbow/go-xsearch)](https://goreportcard.com/report/github.com/mbow/go-xsearch)

</div>

A cold-cache search — including BM25 scoring, typo correction, popularity
ranking, and HTML rendering — completes in **38 microseconds**. A warm cache
hit returns in **2 microseconds**. No database. No Redis. No Elasticsearch.
Just `go run .`

![Search Demo](docs/demo.gif)

## Quick Start

```bash
git clone https://github.com/mbow/go-xsearch.git
cd go-xsearch
go run .
# open http://127.0.0.1:8080
```

Set `LISTEN_ADDR=0.0.0.0:8080` to expose on all interfaces.

## What It Does

Type a few characters and get instant results:

- **"bud"** &rarr; Budweiser (prefix match)
- **"budwiser"** &rarr; Budweiser (typo tolerance)
- **"pod"** &rarr; AirPods Pro (substring match)
- **"beer"** &rarr; most popular beers (category search)
- **"hoppy"** &rarr; IPAs and Pale Ales (tag search)

Results blend **relevance** (how well the query matches) with **popularity**
(what users click). Recent clicks count more; stale popularity fades with
exponential decay.

## Real-World Performance

What users actually experience — full HTTP round-trip including search,
ranking, and template rendering. 10,000 products on an AMD Ryzen 9 5950X.

| Scenario | Latency | Context |
| -------- | ------: | ------- |
| Warm cache hit | **2.2 µs** | Faster than a DNS lookup |
| Cold cache miss | **38 µs** | 440x faster than a single 60 FPS frame (16,667 µs) |
| 32 concurrent searches | **170 ns/op** | Scales linearly under load |

Every search data structure is precomputed at build time and embedded in the
binary. Startup is instant — zero file I/O, zero network calls.

## How It Works

```
Query → Extract trigrams → Bloom pre-check
                             |
                        NO match → category fallback only (524 ns)
                             |
                        YES  → BM25 word match?
                                  |
                             YES  → score + prefix boost + popularity → top 10
                                  |
                             NO   → Jaccard trigram scoring (typo tolerant) → top 10
                                     → category fallback if < 3 results
```

The **Bloom filter** rejects gibberish in ~500 ns — before any scoring runs.
When BM25 finds no word-level matches (typos like "budwiser"), the engine falls
back to **Jaccard trigram scoring**: overlapping 3-character substrings ranked
by set overlap. No edit distance computation needed.

## Tech Stack

| Component | Technology |
| --------- | ---------- |
| Language | Go 1.26 (standard library only) |
| Frontend | HTMX 2.0.8 (vendored, 50 KB) |
| Data format | CBOR via [fxamacker/cbor](https://github.com/fxamacker/cbor) |
| Database | None |
| External services | None |

One external dependency. Everything else — HTTP server, HTML templates, search
algorithms, ranking — is built from scratch with Go's standard library.

---

## Architecture

```
go-xsearch/
  main.go                      # Entry point, graceful shutdown, periodic snapshots
  internal/server/             # HTTP handlers, security middleware, rate limiter, cache
  engine/engine.go             # Search orchestrator, highlight computation
  bm25/bm25.go                # BM25 scoring with prefix boost (typed min-heap)
  bloom/bloom.go               # Bloom filter (probabilistic fast rejection)
  index/ngram.go               # N-gram inverted index (Jaccard + binary search)
  ranking/ranking.go           # Popularity ranking (exponential time decay)
  catalog/                     # Product model + embedded CBOR loader
  cmd/generate/                # Build-time codegen: JSON → gzip CBOR with pre-built indexes
  cmd/generate_products/       # Sample data generator (10K beers + spirits)
  benchmarks/suite_test.go     # 30 benchmarks (single-thread + parallel contention)
  templates/                   # HTMX templates (highlights, ghost text, keyboard nav)
  static/htmx.min.js          # Vendored HTMX
  data/products.json           # Source product catalog
```

## Algorithms

Five search algorithms, all implemented from scratch:

### BM25 with Prefix Boosting

The primary ranking algorithm. Product names, categories, and tags are tokenized
into words with precomputed IDF (inverse document frequency) values. Queries are
scored using the Okapi BM25 formula. A **prefix bonus** (0.5 &times; max IDF) is
added when a query term is a prefix of a product name word — this is why "bud"
ranks Budweiser above Funky Buddha.

### Bloom Filter

A probabilistic data structure that answers "is this item definitely NOT in the
set?" in constant time. Uses FNV-1a and DJB2 hash functions with double hashing.
False positives are possible; false negatives are not.

### N-gram Inverted Index (Jaccard Fallback)

Every product name and tag is broken into overlapping 3-character substrings
(trigrams). An inverted index maps each trigram to the products that contain it.
When BM25 finds no word-level matches (typos like "budwiser"), the system falls
back to scoring by **Jaccard similarity** — the ratio of shared trigrams to
total trigrams. Typo tolerance without edit distance computation.

### Exponential Decay Ranking

Each user click records a timestamp. Popularity is computed as
`sum(e^(-0.05 * age_in_days))` — recent clicks count more, old clicks fade.
This blends with search relevance (70% BM25 + 30% popularity) for the final
ranking.

### Category Fallback

When a query doesn't match any product well enough, the system matches against
category names and returns the most popular products from the best-matching
category.

## Raw Benchmarks

Engine-level numbers for those who want the full picture. Same machine and
dataset as above (10K products, AMD Ryzen 9 5950X).

| Benchmark | Example | Latency | Allocs | Bytes |
| --------- | ------- | ------: | -----: | ----: |
| Cached prefix (1-2 chars) | `"b"` | **13 ns** | 0 | 0 |
| Category match | `"beer"` | **18 ns** | 0 | 0 |
| Prefix (3+ chars) | `"nik"` | **240 ns** | 1 | 16 |
| Bloom rejection | `"xzqwvp"` | **524 ns** | 1 | 64 |
| Fuzzy / typo (Jaccard) | `"budwiser"` | **1.8 µs** | 8 | 1,627 |
| Full BM25 pipeline | `"budweiser"` | **2.5 µs** | 8 | 1,643 |
| HTTP warm cache | `GET /search?q=bud` | **2.2 µs** | 24 | 9,021 |
| HTTP cold cache | `GET /search?q=bud` | **38 µs** | 256 | 118K |
| Parallel (32 goroutines) | mixed queries | **170 ns** | 7 | 902 |

### Key Optimisations

- **Bloom-first pipeline** — rejects gibberish before BM25 or Jaccard runs
- **Single trigram extraction** — computed once, reused by Bloom check, Jaccard
  search, and category lookup
- **Typed min-heap** for top-K — no `container/heap` interface boxing
- **Pooled bitsets** for candidate dedup — zero-alloc on the hot path
- **Stack-allocated buffers** — trigram sort, candidate tracking, and highlight
  arrays avoid heap escape for typical queries
- **Slice-indexed ranking** — `[]float64` by product ID replaces map lookups
- **Sort+compact dedup** — replaces `map[string]struct{}` for trigram sets
- **Parallel prefix cache** — startup scales with CPU cores

## Updating Product Data

Edit `data/products.json`, then regenerate the embedded binary:

```bash
go generate ./catalog/
go build .
```

The generator reads JSON, builds the Bloom filter, n-gram index, and BM25 index,
serializes to gzip-compressed CBOR, and writes `catalog/data.cbor`. This file is
embedded at compile time via `//go:embed`.

## Running Benchmarks

```bash
make bench                   # quick run (all benchmarks)
make bench-record            # record with commit metadata
make bench-save              # save as comparison baseline
make bench-compare           # compare latest vs saved baseline
make bench-compare-baseline  # compare against original pristine baseline

# CPU profile a specific benchmark
go test -bench=BenchmarkComponent_EngineBM25Path -cpuprofile=cpu.prof ./benchmarks/
go tool pprof cpu.prof
```

## Running Tests

```bash
go test ./...          # all tests
go test -race ./...    # with race detector
```

## License

MIT
