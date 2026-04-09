# xsearch Standalone Repo Extraction Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract `xsearch/` into `github.com/mbow/xsearch` as a standalone Go module with preserved git history, then migrate the sample app to import it.

**Architecture:** Two phases. Phase 1 clones the repo, uses `git filter-repo` to extract the subtree, adds `go.mod`/LICENSE/README, pushes to the new remote, and tags `v0.0.1`. Phase 2 deletes `xsearch/` from `go-xsearch`, updates imports, and verifies.

**Tech Stack:** Git, `git-filter-repo`, Go 1.26

**Spec:** `docs/superpowers/specs/2026-04-09-xsearch-repo-extraction.md`

**Remote:** `git@github.com:mbow/xsearch.git` (already created, empty)

---

## Phase 1: Extract and Publish

### Task 1: Install git-filter-repo

- [ ] **Step 1: Install git-filter-repo**

```bash
pipx install git-filter-repo
```

If `pipx` is not available:

```bash
pip3 install --break-system-packages git-filter-repo
```

- [ ] **Step 2: Verify installation**

```bash
git filter-repo --version
```

Expected: version number printed, no error.

---

### Task 2: Extract xsearch subtree with history

- [ ] **Step 1: Clone go-xsearch into a temp directory**

```bash
git clone git@github.com:mbow/go-xsearch.git /tmp/xsearch-extract
cd /tmp/xsearch-extract
```

- [ ] **Step 2: Verify the clone has the xsearch directory**

```bash
ls xsearch/*.go | head -5
```

Expected: lists xsearch Go source files.

- [ ] **Step 3: Run filter-repo to extract the xsearch/ subtree**

```bash
git filter-repo --subdirectory-filter xsearch/
```

This rewrites history so `xsearch/` becomes the repo root. Only commits that touched files inside `xsearch/` are preserved.

- [ ] **Step 4: Verify the extraction worked**

```bash
ls *.go | head -10
git log --oneline | head -10
```

Expected: Go source files at the repo root (`xsearch.go`, `bloom.go`, etc.). Git log shows commits that touched xsearch files.

- [ ] **Step 5: Commit (nothing to commit — filter-repo already rewrote history)**

Verification only. No commit needed.

---

### Task 3: Create go.mod and verify tests

- [ ] **Step 1: Create go.mod**

```bash
cd /tmp/xsearch-extract
go mod init github.com/mbow/xsearch
```

- [ ] **Step 2: Add the CBOR dependency**

```bash
go get github.com/fxamacker/cbor/v2
go mod tidy
```

- [ ] **Step 3: Verify tests pass**

```bash
go test ./... -race -count=1
```

Expected: All tests PASS.

- [ ] **Step 4: Verify go vet passes**

```bash
go vet ./...
```

Expected: No issues.

- [ ] **Step 5: Commit go.mod and go.sum**

```bash
git add go.mod go.sum
git commit -m "chore: add go.mod for github.com/mbow/xsearch"
```

---

### Task 4: Add LICENSE

- [ ] **Step 1: Create LICENSE file**

```bash
cat > LICENSE << 'LICEOF'
MIT License

Copyright (c) 2026 mbow

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
LICEOF
```

- [ ] **Step 2: Commit**

```bash
git add LICENSE
git commit -m "chore: add MIT license"
```

---

### Task 5: Add README

- [ ] **Step 1: Create README.md**

```bash
cat > README.md << 'READMEEOF'
# xsearch

Fuzzy search library for Go. Per-field BM25 and Jaccard trigram scoring with
optional bloom filter pre-rejection. Index any type. Handle typos, prefix
matching, and group fallback — all in-process.

```bash
go get github.com/mbow/xsearch
```

## Usage

Implement `Searchable` on your type:

```go
type Drink struct {
    ID          string
    Name        string
    Category    string
    Ingredients []string
    Tags        map[string][]string
}

func (d Drink) SearchID() string { return d.ID }

func (d Drink) SearchFields() []xsearch.Field {
    fields := []xsearch.Field{
        {Name: "name", Values: []string{d.Name}, Weight: 1.0},
        {Name: "category", Values: []string{d.Category}, Weight: 0.5},
    }
    if len(d.Ingredients) > 0 {
        fields = append(fields, xsearch.Field{
            Name: "ingredients", Values: d.Ingredients, Weight: 0.4,
        })
    }
    for key, vals := range d.Tags {
        fields = append(fields, xsearch.Field{
            Name: key, Values: vals, Weight: 0.3,
        })
    }
    return fields
}
```

Build and search:

```go
engine, err := xsearch.New(drinks,
    xsearch.WithBloom(100),
    xsearch.WithFallbackField("category"),
    xsearch.WithLimit(20),
)

results := engine.Search("smoky scotch")
for _, r := range results {
    item, _ := engine.Get(r.ID)
    fmt.Printf("%s (score: %.3f)\n", item.Fields[0].Values[0], r.Score)
}
```

## Configuration

| Option | Default | Purpose |
| ------ | ------: | ------- |
| `WithBloom(bitsPerItem)` | disabled | Bloom filter pre-rejection |
| `WithBM25(k1, b)` | 1.2, 0.75 | BM25 parameters |
| `WithAlpha(alpha)` | 0.6 | Relevance vs scorer blend weight |
| `WithLimit(n)` | 10 | Max results per search [2, 100] |
| `WithFallbackField(name)` | none | Field for group fallback |

## External Scoring

Pass a `Scorer` per search to blend relevance with popularity or business logic:

```go
results := engine.Search("lager", xsearch.WithScoring(scorer))
```

## Snapshots

Build indices once, serialize to CBOR, reload fast:

```go
data, _ := engine.Snapshot()
engine2, _ := xsearch.NewFromSnapshot(data, items)
```

## License

MIT
READMEEOF
```

Note: the README above uses nested code fences. If your shell has issues with the heredoc, create the file with your editor instead. The content is a stripped-down version of the library sections from the go-xsearch README.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add library README"
```

---

### Task 6: Push and tag

- [ ] **Step 1: Add the new remote**

```bash
cd /tmp/xsearch-extract
git remote add origin git@github.com:mbow/xsearch.git
```

Note: `filter-repo` removes the original remote, so this adds it fresh.

- [ ] **Step 2: Push all history to the new repo**

```bash
git push -u origin main
```

If the default branch is `master` after filter-repo:

```bash
git branch -M main
git push -u origin main
```

- [ ] **Step 3: Tag v0.0.1**

```bash
git tag v0.0.1
git push origin v0.0.1
```

- [ ] **Step 4: Verify the module is fetchable**

```bash
cd /tmp
mkdir xsearch-verify && cd xsearch-verify
go mod init verify
go get github.com/mbow/xsearch@v0.0.1
```

Expected: Downloads successfully. No errors.

- [ ] **Step 5: Clean up temp directories**

```bash
rm -rf /tmp/xsearch-extract /tmp/xsearch-verify
```

---

## Phase 2: Migrate the Sample App

### Task 7: Delete xsearch/ and update imports

**Files:**
- Delete: `xsearch/` (entire directory)
- Modify: `catalog/catalog.go`
- Modify: `catalog/embed_test.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `cmd/generate/main.go`
- Modify: `main.go`
- Modify: `benchmarks/suite_test.go`

- [ ] **Step 1: Add the published library**

```bash
cd /home/mbow/code/search
go get github.com/mbow/xsearch@v0.0.1
```

- [ ] **Step 2: Delete the local xsearch/ directory**

```bash
rm -rf xsearch/
```

- [ ] **Step 3: Update all import paths**

Replace `"github.com/mbow/go-xsearch/xsearch"` with `"github.com/mbow/xsearch"` in all 7 files:

```bash
sed -i 's|"github.com/mbow/go-xsearch/xsearch"|"github.com/mbow/xsearch"|g' \
    catalog/catalog.go \
    catalog/embed_test.go \
    internal/server/server.go \
    internal/server/server_test.go \
    cmd/generate/main.go \
    main.go \
    benchmarks/suite_test.go
```

- [ ] **Step 4: Tidy the module**

```bash
go mod tidy
```

- [ ] **Step 5: Verify tests pass**

```bash
go test ./... -count=1
```

Expected: All PASS.

- [ ] **Step 6: Verify go vet passes**

```bash
go vet ./...
```

Expected: No issues.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: import xsearch from standalone module github.com/mbow/xsearch

Removed local xsearch/ package. The library now lives at
github.com/mbow/xsearch@v0.0.1 as a standalone Go module."
```

---

### Task 8: Update README import paths

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update import paths in README examples**

Replace any occurrence of `"github.com/mbow/go-xsearch/xsearch"` with `"github.com/mbow/xsearch"` in README.md.

```bash
sed -i 's|github.com/mbow/go-xsearch/xsearch|github.com/mbow/xsearch|g' README.md
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: update xsearch import path in README"
```

---

### Task 9: Run benchmarks and verify no regression

- [ ] **Step 1: Run benchmarks**

```bash
go test ./benchmarks/ -bench=. -benchmem -count=5 -timeout 120s
```

Expected: All benchmarks run. Numbers should be identical to pre-extraction (same code, different import path).

- [ ] **Step 2: Verify the app builds and starts**

```bash
go build -o /tmp/go-xsearch .
/tmp/go-xsearch &
curl -s 'http://127.0.0.1:8080/search?q=bud' | head -3
kill %1
rm /tmp/go-xsearch
```

Expected: Server starts, search returns HTML results.
