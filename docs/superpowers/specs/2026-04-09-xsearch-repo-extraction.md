# xsearch Standalone Repo Extraction

**Date:** 2026-04-09
**Status:** Approved

## Goal

Extract the `xsearch/` package from `go-xsearch` into its own standalone repository at `github.com/mbow/xsearch`, preserving git history. The sample app stays in `go-xsearch` and imports the published library.

## Decisions

- **Module path:** `github.com/mbow/xsearch`
- **Package name:** `xsearch` (no stutter — import as `"github.com/mbow/xsearch"`, use as `xsearch.New(...)`)
- **Initial version:** `v0.0.1` — API still evolving, no stability promise
- **History:** Preserved via `git filter-repo --subdirectory-filter xsearch/`
- **Interfaces:** Stay in the root package. No `contract` or `types` sub-package.
- **Sample app:** Stays in `go-xsearch`, imports the published library

## New Repo Structure

```
github.com/mbow/xsearch/
├── go.mod                  # module github.com/mbow/xsearch
├── go.sum
├── xsearch.go              # Engine, Searchable, Scorer, Field, Item, New(), Search(), Get(), IDs()
├── result.go               # Result, MatchType, Highlight
├── config.go               # Option, WithBloom, WithBM25, WithAlpha, WithLimit, WithFallbackField
├── bloom.go                # Bloom filter (exported)
├── ngram.go                # unexported n-gram index
├── bm25.go                 # unexported BM25 index
├── snapshot.go             # CBOR snapshots (XSRC versioned)
├── helpers.go              # normalizeQuery, tokenize, highlights, sanitize
├── xsearch_test.go         # engine integration tests
├── ngram_test.go           # n-gram unit tests
├── bm25_test.go            # BM25 unit tests
├── bloom_test.go           # bloom filter tests
├── snapshot_test.go        # snapshot round-trip tests
├── bench_test.go           # library benchmarks
├── LICENSE                 # MIT (same as go-xsearch)
└── README.md               # library-focused documentation
```

One external dependency: `github.com/fxamacker/cbor/v2`.

## Extraction Steps

### Phase 1: Create the standalone repo

1. Create `github.com/mbow/xsearch` repo on GitHub (empty, no README)
2. Clone `go-xsearch` into a temporary working copy:
   ```bash
   git clone github.com/mbow/go-xsearch /tmp/xsearch-extract
   cd /tmp/xsearch-extract
   ```
3. Extract the `xsearch/` subtree with history:
   ```bash
   git filter-repo --subdirectory-filter xsearch/
   ```
   This rewrites history so `xsearch/` becomes the repo root. Only commits that touched files inside `xsearch/` are preserved.
4. Create `go.mod` (the extracted subtree has no go.mod — it lived under the parent module):
   ```bash
   go mod init github.com/mbow/xsearch
   go get github.com/fxamacker/cbor/v2
   go mod tidy
   ```
5. Fix any import paths inside test files (if they reference the old module path)
6. Add `LICENSE` (MIT, copied from go-xsearch)
7. Add `README.md` (library-focused — see README section below)
8. Verify: `go test ./... -race`
9. Verify: `go vet ./...`
10. Push to `github.com/mbow/xsearch`
11. Tag `v0.0.1`:
    ```bash
    git tag v0.0.1
    git push origin v0.0.1
    ```

### Phase 2: Migrate the sample app

In the original `go-xsearch` repo:

1. Delete the `xsearch/` directory
2. Add the published library:
   ```bash
   go get github.com/mbow/xsearch@v0.0.1
   ```
3. Update all 7 import paths from `"github.com/mbow/go-xsearch/xsearch"` to `"github.com/mbow/xsearch"`:
   - `catalog/catalog.go`
   - `catalog/embed_test.go`
   - `internal/server/server.go`
   - `internal/server/server_test.go`
   - `cmd/generate/main.go`
   - `main.go`
   - `benchmarks/suite_test.go`
4. Run `go mod tidy` to clean up the dependency graph
5. Verify: `go test ./...`
6. Verify: `go vet ./...`
7. Run benchmarks and compare against baseline to confirm no regression
8. Update README.md import paths in code examples

## README for Standalone Library

The new repo's README covers:
- One-line pitch + Go/MIT/CI badges
- `go get github.com/mbow/xsearch` install
- `Searchable` interface example (drinks schema from current README)
- Engine construction + search example
- Configuration options table
- External scoring with `WithScoring()`
- CBOR snapshots (`Snapshot()` / `NewFromSnapshot()`)
- Bloom filter standalone usage
- Scoring model summary (per-field BM25, Jaccard, fallback groups)
- Library benchmarks
- License

No sample app content, no HTTP/HTMX details, no server benchmarks.

## What Stays in go-xsearch

Everything except `xsearch/`:
- `catalog/` — product model, implements `xsearch.Searchable`
- `ranking/` — popularity scorer, implements `xsearch.Scorer`
- `internal/server/` — HTTP handlers, HTMX, caching, rate limiting
- `cmd/generate/` — JSON to CBOR index generation
- `main.go` — wires library + server + ranking
- `benchmarks/` — full-stack regression suite
- `templates/`, `static/`, `data/`
- All docs and specs

## Risks and Mitigations

**Risk:** `git filter-repo` rewrites commit SHAs. The new repo's history won't have matching SHAs to the original.
**Mitigation:** Acceptable — the original repo retains unmodified history. Cross-reference by date/message if needed.

**Risk:** `go.sum` mismatch after module path change.
**Mitigation:** `go mod tidy` regenerates it. Tests verify correctness.

**Risk:** Benchmarks in `go-xsearch` may show import-path-related noise.
**Mitigation:** The code is identical — run benchmarks to confirm, then save as new baseline.
