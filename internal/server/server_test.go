package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
)

func testEngine() *engine.Engine {
	products := []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
	}
	return engine.New(products)
}

func testApp() *App {
	app := New(testEngine(), 64)
	app.TemplateDir = "../../templates"
	app.LoadTemplates()
	return app
}

func TestHandleIndex(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	app.HandleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Product Search") {
		t.Error("expected page to contain 'Product Search'")
	}
	if !strings.Contains(body, "hx-get") {
		t.Error("expected page to contain HTMX attributes")
	}
}

func TestHandleSearch(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest("GET", "/search?q=nik", nil)
	w := httptest.NewRecorder()
	app.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Nik") || !strings.Contains(body, "Air Max") {
		t.Error("expected results to contain 'Nike Air Max' (possibly highlighted)")
	}
}

func TestHandleSearchEmpty(t *testing.T) {
	app := testApp()

	req := httptest.NewRequest("GET", "/search?q=", nil)
	w := httptest.NewRecorder()
	app.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelect(t *testing.T) {
	app := testApp()

	body := strings.NewReader(`{"id": "0"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.HandleSelect(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelectInvalidID(t *testing.T) {
	app := testApp()

	body := strings.NewReader(`{"id": "abc"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.HandleSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- HTTP handler benchmarks ---

func benchEngine(b *testing.B) *engine.Engine {
	b.Helper()
	products, err := catalog.EmbeddedProducts()
	if err != nil {
		b.Fatal(err)
	}
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

func BenchmarkHTTPSearch_ColdCache(b *testing.B) {
	e := benchEngine(b)
	app := New(e, 1024)
	app.TemplateDir = "../../templates"
	app.LoadTemplates()
	b.ResetTimer()
	for b.Loop() {
		app.Cache.Invalidate()
		req := httptest.NewRequest("GET", "/search?q=bud", nil)
		w := httptest.NewRecorder()
		app.HandleSearch(w, req)
	}
}

func BenchmarkHTTPSearch_WarmCache(b *testing.B) {
	e := benchEngine(b)
	app := New(e, 1024)
	app.TemplateDir = "../../templates"
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

func BenchmarkHTTPSelect(b *testing.B) {
	e := benchEngine(b)
	app := New(e, 1024)
	app.TemplateDir = "../../templates"
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
