package main

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

func TestHandleIndex(t *testing.T) {
	app := &App{engine: testEngine(), cache: newFragmentCache(64)}
	app.loadTemplates()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	app.handleIndex(w, req)

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
	app := &App{engine: testEngine(), cache: newFragmentCache(64)}
	app.loadTemplates()

	req := httptest.NewRequest("GET", "/search?q=nik", nil)
	w := httptest.NewRecorder()
	app.handleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Nik") || !strings.Contains(body, "Air Max") {
		t.Error("expected results to contain 'Nike Air Max' (possibly highlighted)")
	}
}

func TestHandleSearchEmpty(t *testing.T) {
	app := &App{engine: testEngine(), cache: newFragmentCache(64)}
	app.loadTemplates()

	req := httptest.NewRequest("GET", "/search?q=", nil)
	w := httptest.NewRecorder()
	app.handleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelect(t *testing.T) {
	app := &App{engine: testEngine(), cache: newFragmentCache(64)}

	body := strings.NewReader(`{"id": "0"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.handleSelect(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSelectInvalidID(t *testing.T) {
	app := &App{engine: testEngine(), cache: newFragmentCache(64)}

	body := strings.NewReader(`{"id": "abc"}`)
	req := httptest.NewRequest("POST", "/select", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.handleSelect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
