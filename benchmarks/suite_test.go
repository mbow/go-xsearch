package benchmarks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbow/go-xsearch/bloom"
	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
	"github.com/mbow/go-xsearch/index"
	"github.com/mbow/go-xsearch/internal/server"
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

// =====================================================================
// 1. HTTP Server Layer Benchmarks
// =====================================================================

func BenchmarkHTTPServer_Search_ColdCache(b *testing.B) {
	e := benchEngine(b)
	app := server.New(e, 1024)
	app.TemplateDir = "../templates" // adjusted for relative path from benchmarks/
	app.LoadTemplates()

	b.ResetTimer()
	for b.Loop() {
		app.Cache.Invalidate()
		req := httptest.NewRequest("GET", "/search?q=bud", nil)
		w := httptest.NewRecorder()
		app.HandleSearch(w, req)
	}
}

func BenchmarkHTTPServer_Search_WarmCache(b *testing.B) {
	e := benchEngine(b)
	app := server.New(e, 1024)
	app.TemplateDir = "../templates"
	app.LoadTemplates()

	// prime cache
	req := httptest.NewRequest("GET", "/search?q=bud", nil)
	w := httptest.NewRecorder()
	app.HandleSearch(w, req)

	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("GET", "/search?q=bud", nil)
		w := httptest.NewRecorder()
		app.HandleSearch(w, req)
	}
}

func BenchmarkHTTPServer_Select(b *testing.B) {
	e := benchEngine(b)
	app := server.New(e, 1024)
	app.TemplateDir = "../templates"
	app.LoadTemplates()

	b.ResetTimer()
	for b.Loop() {
		body := strings.NewReader(`{"id": "0"}`)
		req := httptest.NewRequest("POST", "/select", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.HandleSelect(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d", w.Code)
		}
	}
}

// =====================================================================
// 2. Raw Main Search Algorithm Benchmarks (Engine Layer)
// =====================================================================

func BenchmarkEngine_Search_Prefix3Char(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("nik")
	}
}

func BenchmarkEngine_Search_Fuzzy(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("budwiser")
	}
}

func BenchmarkEngine_Search_CachedPrefix(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("b")
	}
}

func BenchmarkEngine_Search_CategoryFallback(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("beer")
	}
}

func BenchmarkEngine_Search_BloomReject(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("xzqwvp")
	}
}

func BenchmarkEngine_Search_WithPopularity(b *testing.B) {
	e := benchEngine(b)
	for range 100 {
		e.RecordSelection(0)
	}
	b.ResetTimer()
	for b.Loop() {
		e.Search("budweiser")
	}
}

// =====================================================================
// 3. Separate Components Benchmarks
// =====================================================================

func BenchmarkComponent_EngineShortQuery(b *testing.B) {
	// Directly tests the sub-2 char query path which hits prefix cache
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("bu")
	}
}

func BenchmarkComponent_EnginePrefixBoost(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("bud")
	}
}

func BenchmarkComponent_EngineBM25Path(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("budweiser")
	}
}

func BenchmarkComponent_EngineJaccardFallback(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for b.Loop() {
		e.Search("budwiser")
	}
}

// --- Lower level component benchmarks (moved from old tests) ---

func BenchmarkBloom_MayContain(b *testing.B) {
	bf := bloom.New(20000, 3)
	bf.Add("bud")
	bf.Add("udw")
	bf.Add("dwe")
	b.ResetTimer()
	for b.Loop() {
		bf.MayContain("bud")
	}
}

func BenchmarkBloom_Miss(b *testing.B) {
	bf := bloom.New(20000, 3)
	bf.Add("bud")
	b.ResetTimer()
	for b.Loop() {
		bf.MayContain("zzz")
	}
}

func BenchmarkIndex_ExtractTrigrams_Short(b *testing.B) {
	for b.Loop() {
		index.ExtractTrigrams("bud")
	}
}

func BenchmarkIndex_ExtractTrigrams_Medium(b *testing.B) {
	for b.Loop() {
		index.ExtractTrigrams("budweiser")
	}
}

func BenchmarkIndex_ExtractTrigrams_Long(b *testing.B) {
	for b.Loop() {
		index.ExtractTrigrams("weihenstephaner hefeweissbier")
	}
}

func BenchmarkIndex_Search_Prefix(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("nik")
	}
}

func BenchmarkIndex_Search_Fuzzy(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("budwiser")
	}
}

func BenchmarkIndex_Search_Exact(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("budweiser")
	}
}

func BenchmarkIndex_Search_ShortQuery(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.Search("b")
	}
}

func BenchmarkIndex_SearchCategories(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		idx.SearchCategories("beer")
	}
}

func BenchmarkIndex_BestCategory(b *testing.B) {
	products := benchProducts(b)
	idx := index.NewIndex(products)
	b.ResetTimer()
	for b.Loop() {
		_, _ = idx.BestCategory("beer")
	}
}

func BenchmarkRanking_CombinedScore(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	for range 50 {
		r.RecordSelection(0)
	}
	b.ResetTimer()
	for b.Loop() {
		r.CombinedScore(0, 0.8)
	}
}

func BenchmarkRanking_CombinedScore_NoSelections(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	b.ResetTimer()
	for b.Loop() {
		r.CombinedScore(0, 0.8)
	}
}

func BenchmarkRanking_Scorer(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	for range 50 {
		r.RecordSelection(0)
	}
	b.ResetTimer()
	for b.Loop() {
		score := r.Scorer()
		_ = score(0, 0.8)
	}
}

// =====================================================================
// 4. Parallel Contention Benchmarks
// =====================================================================

func BenchmarkParallel_EngineSearch(b *testing.B) {
	e := benchEngine(b)
	queries := []string{"bud", "budweiser", "nik", "beer", "xzqwvp"}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			e.Search(queries[i%len(queries)])
			i++
		}
	})
}

func BenchmarkParallel_HTTPSearch(b *testing.B) {
	e := benchEngine(b)
	app := server.New(e, 1024)
	app.TemplateDir = "../templates"
	app.LoadTemplates()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/search?q=bud", nil)
			w := httptest.NewRecorder()
			app.HandleSearch(w, req)
		}
	})
}

func BenchmarkParallel_SearchWithSelections(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				e.RecordSelection(i % 100)
			}
			e.Search("budweiser")
			i++
		}
	})
}
