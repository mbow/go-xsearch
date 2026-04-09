# go-xsearch

<p align="center">
  <img src="docs/logo.png" alt="go-xsearch" width="180" />
</p>

<p align="center">
  <strong>Sample application for the <a href="https://github.com/mbow/xsearch">xsearch</a> fuzzy search library.</strong>
</p>

<p align="center">
  <a href="https://github.com/mbow/go-xsearch/actions/workflows/ci.yml">
    <img alt="CI" src="https://github.com/mbow/go-xsearch/actions/workflows/ci.yml/badge.svg" />
  </a>
  <a href="https://go.dev">
    <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" />
  </a>
  <a href="LICENSE">
    <img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-yellow.svg" />
  </a>
  <a href="https://goreportcard.com/report/github.com/mbow/go-xsearch">
    <img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/mbow/go-xsearch" />
  </a>
</p>

A complete HTMX web application demonstrating
[xsearch](https://github.com/mbow/xsearch) in production: per-field BM25
scoring, typo correction, popularity ranking, fragment caching, rate limiting,
and graceful shutdown. A warm-cache search returns in **2.4 microseconds**.

<p align="center">
  <img src="docs/demo.gif" alt="Search Demo" />
</p>

## The Library

The search engine lives in its own repository:

```bash
go get github.com/mbow/xsearch
```

See [github.com/mbow/xsearch](https://github.com/mbow/xsearch) for the full
library documentation, API reference, and standalone usage examples.

This sample app demonstrates how to integrate the library into an HTTP service
with popularity ranking, CBOR snapshot loading, and HTMX-driven UI.

## Quick Start

```bash
git clone https://github.com/mbow/go-xsearch.git
cd go-xsearch
go run .
# open http://127.0.0.1:8080
```

Set `LISTEN_ADDR=0.0.0.0:8080` to expose on all interfaces.

Type a few characters and get instant results:

- **"bud"** &rarr; Budweiser (prefix match)
- **"budwiser"** &rarr; Budweiser (typo tolerance)
- **"beer"** &rarr; most popular beers (category fallback)
- **"hoppy"** &rarr; IPAs and Pale Ales (tag search)

Results blend **relevance** (how well the query matches) with **popularity**
(what users click). Recent clicks count more; stale popularity fades with
exponential decay.

## Architecture

```
go-xsearch/                            # THIS REPO — sample application
  catalog/                             # Product model, implements xsearch.Searchable
  ranking/                             # Popularity scorer, implements xsearch.Scorer
  internal/server/                     # HTTP handlers, HTMX, caching, rate limiting
  cmd/generate/                        # JSON -> CBOR index generation
  main.go                              # Wires xsearch + server + ranking + prefix cache
  benchmarks/suite_test.go             # Performance regression suite
  templates/                           # HTMX templates (highlights, ghost text, keyboard nav)
  static/htmx.min.js                  # Vendored HTMX 2.0.8
  data/products.json                   # Source product catalog (10K items)

github.com/mbow/xsearch               # THE LIBRARY — standalone module
  Engine, Searchable, Scorer, Field    # Core types and interfaces
  WithBloom, WithBM25, WithLimit, ...  # Functional options
  Bloom filter, BM25, n-gram index     # Search internals
  Snapshot / NewFromSnapshot           # CBOR serialization
```

This app imports `github.com/mbow/xsearch` as an external dependency. The
library has no knowledge of products, HTTP, or HTMX.

## How It Works

```
Query -> xsearch.Engine.Search()
           |
           +-> Normalize -> Prefix cache hit? (no scorer) -> return cached results
           +-> Extract trigrams -> Bloom pre-check
                                                  |
                                             NO match -> fallback group only
                                                  |
                                             YES  -> BM25 per-field scoring?
                                                       |
                                                  YES  -> weighted sum + prefix boost -> top K
                                                       |
                                                  NO   -> Jaccard per-field scoring -> top K
                                                           -> fallback group if < 3 results
           |
           +-> Server applies WithScoring(ranker.ScoreView()) per request
           +-> Renders HTMX fragment with highlights
           +-> FragmentCache stores rendered HTML
```

The library handles search and scoring. This app adds:
- **Popularity ranking** — exponential time-decay scorer passed via `WithScoring()` per request
- **Fragment caching** — rendered HTML cached by query string, invalidated on selection
- **Rate limiting** — per-IP token bucket
- **HTMX rendering** — highlight spans, ghost text completion, keyboard navigation

## Performance

Full HTTP round-trip benchmarks on 10,000 products, AMD Ryzen 9 5950X.

### HTTP Layer

| Scenario | Latency | Allocs | Bytes |
| -------- | ------: | -----: | ----: |
| Warm cache hit | **2.4 us** | 24 | 9,621 |
| Cold cache miss | **13.8 us** | 76 | 21 KiB |
| Selection POST | **2.8 us** | 31 | 7.3 KiB |
| Parallel HTTP (32 goroutines) | **1.2 us** | 24 | 9,622 |

### Engine Layer (via xsearch library)

| Benchmark | Example | Latency | Allocs | Bytes |
| --------- | ------- | ------: | -----: | ----: |
| Prefix (3+ chars) | `"nik"` | **78 ns** | 1 | 16 |
| Bloom rejection | `"xzqwvp"` | **154 ns** | 1 | 16 |
| Fuzzy / typo (Jaccard) | `"budwiser"` | **1.43 us** | 4 | 96 |
| BM25 pipeline | `"budweiser"` | **2.08 us** | 4 | 96 |
| BM25 with popularity | `"budweiser"` | **2.20 us** | 5 | 128 |
| Bloom MayContain | hit | **6.5 ns** | 0 | 0 |
| Bloom Miss | miss | **4.2 ns** | 0 | 0 |
| Ranking score lookup | single item | **3.6 ns** | 0 | 0 |
| Parallel engine (32 goroutines) | mixed queries | **14.9 us** | 24 | 161 KiB |

### Key Optimisations

- **Prefix cache** — precomputed results for 1-2 char queries return in ~28ns with 1 alloc; bypassed when external scoring is active
- **Bloom-first pipeline** — rejects gibberish before scoring runs
- **Per-field inverted indices** — each field maintains its own posting lists
  and IDF tables
- **Slice-indexed scoring** — `Scorer.Score(docIndex int)` avoids map lookups
  on the hot path
- **Request-scoped scoring** — `WithScoring()` passes an immutable snapshot
  per search; no locks in the scoring path
- **Direct HTMX fragment rendering** — cache-miss responses skip template
  reflection on the hot path
- **Fast ASCII paths** — query normalization avoids allocation when the input
  is already lowercase ASCII

## Updating Product Data

Edit `data/products.json`, then regenerate:

```bash
go generate ./catalog/
go build .
```

The generator builds an `xsearch.Engine`, calls `Snapshot()`, and embeds the
self-contained CBOR blob at compile time via `//go:embed`.

## Running Benchmarks

```bash
make bench                   # quick run
make bench-record            # record with commit metadata
make bench-save              # save as comparison baseline
make bench-compare           # compare latest vs saved baseline
```

## Running Tests

```bash
go test ./...          # all tests
go test -race ./...    # with race detector
```

## License

MIT
