package benchmarks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/internal/server"
	"github.com/mbow/go-xsearch/ranking"
	"github.com/mbow/xsearch"
)

func benchRuntime(b *testing.B) (*xsearch.Engine, *ranking.Ranker) {
	b.Helper()
	products, err := catalog.LoadProducts("../data/products.json")
	if err != nil {
		b.Fatal(err)
	}
	snapshot, err := catalog.EmbeddedSnapshot()
	if err != nil {
		b.Fatal(err)
	}
	prefixes := catalog.ExtractPrefixes(products)
	ranker := ranking.New(0.05, 0.6)
	engine, err := xsearch.NewFromSnapshot(snapshot, products,
		xsearch.WithLimit(10),
		xsearch.WithPrefixCache(prefixes),
	)
	if err != nil {
		b.Fatal(err)
	}
	ranker.SetIDs(engine.IDs())
	return engine, ranker
}

// =====================================================================
// 1. HTTP Server Layer Benchmarks
// =====================================================================

func BenchmarkHTTPServer_Search_ColdCache(b *testing.B) {
	engine, ranker := benchRuntime(b)
	app := server.New(engine, ranker, 1024)
	products, err := catalog.LoadProducts("../data/products.json")
	if err != nil {
		b.Fatal(err)
	}
	app.Lookup = server.StaticLookup(products)
	app.TemplateDir = "../templates"
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
	engine, ranker := benchRuntime(b)
	app := server.New(engine, ranker, 1024)
	products, err := catalog.LoadProducts("../data/products.json")
	if err != nil {
		b.Fatal(err)
	}
	app.Lookup = server.StaticLookup(products)
	app.TemplateDir = "../templates"
	app.LoadTemplates()

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
	engine, ranker := benchRuntime(b)
	app := server.New(engine, ranker, 1024)
	products, err := catalog.LoadProducts("../data/products.json")
	if err != nil {
		b.Fatal(err)
	}
	app.Lookup = server.StaticLookup(products)
	app.TemplateDir = "../templates"
	app.LoadTemplates()
	id := engine.IDs()[0]

	b.ResetTimer()
	for b.Loop() {
		body := strings.NewReader(`{"id": "` + id + `"}`)
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
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("nik")
	}
}

func BenchmarkEngine_Search_Fuzzy(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("budwiser")
	}
}

func BenchmarkEngine_Search_CachedPrefix(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("b")
	}
}

func BenchmarkEngine_Search_CategoryFallback(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("beer")
	}
}

func BenchmarkEngine_Search_BloomReject(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("xzqwvp")
	}
}

func BenchmarkEngine_Search_WithPopularity(b *testing.B) {
	engine, ranker := benchRuntime(b)
	id := engine.IDs()[0]
	for range 100 {
		ranker.RecordSelection(id)
	}
	b.ResetTimer()
	for b.Loop() {
		engine.Search("budweiser", xsearch.WithScoring(ranker.ScoreView()))
	}
}

// =====================================================================
// 3. Separate Components Benchmarks
// =====================================================================

func BenchmarkComponent_EngineShortQuery(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("bu")
	}
}

func BenchmarkComponent_EnginePrefixBoost(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("bud")
	}
}

func BenchmarkComponent_EngineBM25Path(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("budweiser")
	}
}

func BenchmarkComponent_EngineJaccardFallback(b *testing.B) {
	engine, _ := benchRuntime(b)
	b.ResetTimer()
	for b.Loop() {
		engine.Search("budwiser")
	}
}

// --- Bloom filter benchmarks (moved from bloom/) ---

func BenchmarkBloom_MayContain(b *testing.B) {
	bf := xsearch.NewBloom(20000, 1)
	bf.Add("bud")
	bf.Add("udw")
	bf.Add("dwe")
	b.ResetTimer()
	for b.Loop() {
		bf.MayContain("bud")
	}
}

func BenchmarkBloom_Miss(b *testing.B) {
	bf := xsearch.NewBloom(20000, 1)
	bf.Add("bud")
	b.ResetTimer()
	for b.Loop() {
		bf.MayContain("zzz")
	}
}

// --- Ranking benchmarks (moved from ranking/) ---

func BenchmarkRanking_CombinedScore(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	r.SetIDs([]string{"item-0"})
	for range 50 {
		r.RecordSelection("item-0")
	}
	b.ResetTimer()
	for b.Loop() {
		r.Score(0)
	}
}

func BenchmarkRanking_CombinedScore_NoSelections(b *testing.B) {
	r := ranking.New(0.05, 0.6)
	r.SetIDs([]string{"item-0"})
	b.ResetTimer()
	for b.Loop() {
		r.Score(0)
	}
}

// =====================================================================
// 4. Parallel Contention Benchmarks
// =====================================================================

func BenchmarkParallel_EngineSearch(b *testing.B) {
	engine, _ := benchRuntime(b)
	queries := []string{"bud", "budweiser", "nik", "beer", "xzqwvp"}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			engine.Search(queries[i%len(queries)])
			i++
		}
	})
}

func BenchmarkParallel_HTTPSearch(b *testing.B) {
	engine, ranker := benchRuntime(b)
	app := server.New(engine, ranker, 1024)
	products, err := catalog.LoadProducts("../data/products.json")
	if err != nil {
		b.Fatal(err)
	}
	app.Lookup = server.StaticLookup(products)
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
	engine, ranker := benchRuntime(b)
	ids := engine.IDs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				ranker.RecordSelection(ids[i%len(ids)])
			}
			engine.Search("budweiser", xsearch.WithScoring(ranker.ScoreView()))
			i++
		}
	})
}
