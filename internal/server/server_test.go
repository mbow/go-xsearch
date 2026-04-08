package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/ranking"
	"github.com/mbow/go-xsearch/xsearch"
)

func testProducts() []catalog.Product {
	return []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
	}
}

func testEngine(t *testing.T) (*xsearch.Engine, *ranking.Ranker) {
	t.Helper()
	ranker := ranking.New(0.05, 0.6)
	engine, err := xsearch.New(testProducts(),
		xsearch.WithFallbackField("category"),
	)
	if err != nil {
		t.Fatal(err)
	}
	ranker.SetIDs(engine.IDs())
	return engine, ranker
}

func testApp(t *testing.T) *App {
	t.Helper()
	engine, ranker := testEngine(t)
	app := New(engine, ranker, 64)
	app.TemplateDir = "../../templates"
	app.LoadTemplates()
	return app
}

func TestHandleIndex(t *testing.T) {
	t.Parallel()
	app := testApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	app.HandleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Product Search") {
		t.Fatal("expected page to contain Product Search")
	}
}

func TestHandleSearch(t *testing.T) {
	t.Parallel()
	app := testApp(t)

	req := httptest.NewRequest("GET", "/search?q=nik", nil)
	w := httptest.NewRecorder()
	app.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Nik") || !strings.Contains(body, "Air Max") {
		t.Fatalf("expected highlighted Nike Air Max, got %q", body)
	}
}

func TestHandleSearchFallbackSection(t *testing.T) {
	t.Parallel()
	app := testApp(t)

	req := httptest.NewRequest("GET", "/search?q=beer", nil)
	w := httptest.NewRecorder()
	app.HandleSearch(w, req)

	if !strings.Contains(w.Body.String(), "Related products") {
		t.Fatal("expected fallback section")
	}
}

func TestHandleSearchNoResults(t *testing.T) {
	t.Parallel()
	app := testApp(t)

	req := httptest.NewRequest("GET", "/search?q=zz", nil)
	w := httptest.NewRecorder()
	app.HandleSearch(w, req)

	if !strings.Contains(w.Body.String(), "No results found") {
		t.Fatal("expected no results section")
	}
}

func TestHandleSelect(t *testing.T) {
	t.Parallel()
	app := testApp(t)
	id := catalog.StableID(testProducts()[0])

	body := strings.NewReader(`{"id": "` + id + `"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.HandleSelect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelectInvalidID(t *testing.T) {
	t.Parallel()
	app := testApp(t)

	body := strings.NewReader(`{"id": "missing"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.HandleSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
