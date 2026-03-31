package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbow/go-xsearch/bloom"
	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
	"github.com/mbow/go-xsearch/index"
	"github.com/mbow/go-xsearch/ranking"
)

func benchProducts(b *testing.B) []catalog.Product {
	b.Helper()
	products, err := catalog.EmbeddedProducts()
	if err != nil {
		b.Fatal(err)
	}
	return products
}

func benchEngine(b *testing.B) *engine.Engine {
	b.Helper()
	products := benchProducts(b)
	bloomRaw, err := catalog.EmbeddedBloomRaw()
	if err != nil {
		b.Fatal(err)
	}
	indexRaw, err := catalog.EmbeddedIndexRaw()
	if err != nil {
		b.Fatal(err)
	}
	bm25Raw, err := catalog.EmbeddedBM25Raw()
	if err != nil {
		b.Fatal(err)
	}
	e, err := engine.NewFromEmbedded(products, bloomRaw, indexRaw, bm25Raw)
	if err != nil {
		b.Fatal(err)
	}
	return e
}

// --- Bloom filter benchmarks ---

func BenchmarkBloomMayContain(b *testing.B) {
	bf := bloom.New(20000, 3)
	bf.Add("bud")
	bf.Add("udw")
	bf.Add("dwe")
	b.ResetTimer()
	for b.Loop() {
		bf.MayContain("bud")
	}
}

func BenchmarkBloomMiss(b *testing.B) {
	bf := bloom.New(20000, 3)
	bf.Add("bud")
	b.ResetTimer()
	for b.Loop() {
		bf.MayContain("zzz")
	}
}

// --- Trigram extraction benchmarks ---

func BenchmarkExtractTrigrams_Short(b *testing.B) {
	for b.Loop() {
		index.ExtractTrigrams("bud")
	}
}

func BenchmarkExtractTrigrams_Medium(b *testing.B) {
	for b.Loop() {
		index.ExtractTrigrams("budweiser")
	}
}

func BenchmarkExtractTrigrams_Long(b *testing.B) {
	for b.Loop() {
		index.ExtractTrigrams("weihenstephaner hefeweissbier")
	}
}

// --- Index search benchmarks ---

func BenchmarkIndexSearch_Prefix(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("nik")
	}
}

func BenchmarkIndexSearch_Fuzzy(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("budwiser")
	}
}

func BenchmarkIndexSearch_Exact(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("budweiser")
	}
}

func BenchmarkIndexSearch_ShortQuery(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("b")
	}
}

func BenchmarkIndexSearchCategories(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.SearchCategories("beer")
	}
}

// --- Ranking benchmarks ---

func BenchmarkRankingCombinedScore(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	for range 50 {
		r.RecordSelection(0)
	}
	b.ResetTimer()
	for b.Loop() {
		r.CombinedScore(0, 0.8)
	}
}

func BenchmarkRankingCombinedScore_NoSelections(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	b.ResetTimer()
	for b.Loop() {
		r.CombinedScore(0, 0.8)
	}
}

// --- Full engine search benchmarks ---

func BenchmarkEngineSearch_Prefix3Char(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("nik")
	}
}

func BenchmarkEngineSearch_Fuzzy(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("budwiser")
	}
}

func BenchmarkEngineSearch_CachedPrefix(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("b")
	}
}

func BenchmarkEngineSearch_CategoryFallback(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("beer")
	}
}

func BenchmarkEngineSearch_BloomReject(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("xzqwvp")
	}
}

func BenchmarkEngineSearch_WithPopularity(b *testing.B) {
	e := benchEngine(b)
	for range 100 {
		e.RecordSelection(0)
	}
	b.ResetTimer()
	for b.Loop() {
		e.Search("budweiser")
	}
}

// --- BM25 path benchmarks ---

func BenchmarkEngineSearch_BM25Path(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("budweiser")
	}
}

func BenchmarkEngineSearch_JaccardFallback(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("budwiser")
	}
}

func BenchmarkEngineSearch_PrefixBoost(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("bud")
	}
}

// --- HTTP handler benchmarks (full round-trip minus network) ---

func BenchmarkHTTPSearch_ColdCache(b *testing.B) {
	e := benchEngine(b)
	app := &App{engine: e, cache: newFragmentCache(1024)}
	app.loadTemplates()
	b.ResetTimer()
	for b.Loop() {
		app.cache.invalidate()
		req := httptest.NewRequest("GET", "/search?q=bud", nil)
		w := httptest.NewRecorder()
		app.handleSearch(w, req)
	}
}

func BenchmarkHTTPSearch_WarmCache(b *testing.B) {
	e := benchEngine(b)
	app := &App{engine: e, cache: newFragmentCache(1024)}
	app.loadTemplates()
	req := httptest.NewRequest("GET", "/search?q=bud", nil)
	w := httptest.NewRecorder()
	app.handleSearch(w, req)
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("GET", "/search?q=bud", nil)
		w := httptest.NewRecorder()
		app.handleSearch(w, req)
	}
}

func BenchmarkHTTPSelect(b *testing.B) {
	e := benchEngine(b)
	app := &App{engine: e, cache: newFragmentCache(1024)}
	b.ResetTimer()
	for b.Loop() {
		body := strings.NewReader(`{"id": "0"}`)
		req := httptest.NewRequest("POST", "/select", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.handleSelect(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d", w.Code)
		}
	}
}
