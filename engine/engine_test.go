package engine

import (
	"github.com/mbow/go-xsearch/catalog"
	"testing"
)

func testProducts() []catalog.Product {
	return []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Coors Light", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
		{Name: "Nike Dunk Low", Category: "shoes"},
		{Name: "AirPods Pro", Category: "headphones"},
	}
}

func TestNewEngine(t *testing.T) {
	e := New(testProducts())
	if e == nil {
		t.Fatal("New() returned nil")
	}
}

func TestSearchDirectMatch(t *testing.T) {
	e := New(testProducts())
	results := e.Search("nike")
	if len(results) == 0 {
		t.Fatal("expected results for 'nike'")
	}

	foundDirect := false
	for _, r := range results {
		if r.MatchType == MatchDirect {
			foundDirect = true
		}
	}
	if !foundDirect {
		t.Error("expected direct match results for 'nike'")
	}
}

func TestSearchBloomRejection(t *testing.T) {
	e := New(testProducts())
	// Completely unrelated query — Bloom should reject most/all trigrams
	results := e.Search("xzqwvp")
	// May still get category fallback results, but no direct matches
	for _, r := range results {
		if r.MatchType == MatchDirect {
			t.Error("expected no direct matches for gibberish query")
		}
	}
}

func TestSearchCategoryFallback(t *testing.T) {
	e := New(testProducts())
	results := e.Search("beer")
	if len(results) == 0 {
		t.Fatal("expected results for 'beer'")
	}

	// "beer" matches the category name — should find beer products
	foundBeer := false
	for _, r := range results {
		if r.Product.Category == "beer" {
			foundBeer = true
		}
	}
	if !foundBeer {
		t.Error("expected beer products in results")
	}
}

func TestSearchFuzzyMatch(t *testing.T) {
	e := New(testProducts())
	results := e.Search("budwiser")
	foundBud := false
	for _, r := range results {
		if r.Product.Name == "Budweiser" {
			foundBud = true
		}
	}
	if !foundBud {
		t.Error("expected to find Budweiser via fuzzy match for 'budwiser'")
	}
}

func TestSearchSortedByScore(t *testing.T) {
	e := New(testProducts())
	results := e.Search("nike")
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: score[%d]=%f > score[%d]=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestSearchRecordAndRank(t *testing.T) {
	e := New(testProducts())

	// Select Nike Dunk Low several times
	for range 10 {
		e.RecordSelection(4) // Nike Dunk Low
	}

	results := e.Search("nike")
	if len(results) < 2 {
		t.Fatal("expected at least 2 results for 'nike'")
	}

	// Nike Dunk Low should rank higher than Nike Air Max due to popularity
	dunkIdx := -1
	airIdx := -1
	for i, r := range results {
		if r.Product.Name == "Nike Dunk Low" {
			dunkIdx = i
		}
		if r.Product.Name == "Nike Air Max" {
			airIdx = i
		}
	}

	if dunkIdx == -1 || airIdx == -1 {
		t.Fatal("expected both Nike products in results")
	}
	if dunkIdx > airIdx {
		t.Error("Nike Dunk Low should rank higher than Nike Air Max due to popularity")
	}
}

func TestSearchHighlighting(t *testing.T) {
	products := []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Bud Light", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
	}

	e := New(products)
	results := e.Search("bud")
	if len(results) == 0 {
		t.Fatal("expected results for 'bud'")
	}

	first := results[0]
	if len(first.Highlights) == 0 {
		t.Error("expected highlights on first result")
	}
	if first.Highlights[0].Start != 0 {
		t.Errorf("expected highlight start at 0, got %d", first.Highlights[0].Start)
	}
	if first.Highlights[0].End != 3 {
		t.Errorf("expected highlight end at 3, got %d", first.Highlights[0].End)
	}
	if first.HighlightedName == "" {
		t.Error("expected non-empty HighlightedName")
	}
}

func TestSearchBudRanking(t *testing.T) {
	products := []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Bud Light", Category: "beer"},
		{Name: "Funky Buddha Double Lambic", Category: "beer"},
		{Name: "Funky Buddha Cask Kolsch", Category: "beer"},
		{Name: "Funky Buddha Original Lambic", Category: "beer"},
		{Name: "Funky Buddha Small Batch Lambic", Category: "beer"},
		{Name: "Funky Buddha No. 12 Light Lager", Category: "beer"},
		{Name: "Funky Buddha Original Light Lager", Category: "beer"},
		{Name: "Funky Buddha No. 1 Brown Ale", Category: "beer"},
		{Name: "Funky Buddha No. 5 Lager", Category: "beer"},
		{Name: "Funky Buddha No. 12 Porter", Category: "beer"},
		{Name: "Funky Buddha Limited Lambic", Category: "beer"},
	}

	e := New(products)
	results := e.Search("bud")

	if len(results) == 0 {
		t.Fatal("expected results for 'bud'")
	}

	// First result must be Budweiser or Bud Light (prefix match on "bud")
	firstName := results[0].Product.Name
	if firstName != "Budweiser" && firstName != "Bud Light" {
		t.Errorf("expected first result to be Budweiser or Bud Light, got %q", firstName)
	}

	// No Funky Buddha product should appear before all Bud* products
	lastBudIdx := -1
	firstFunkyIdx := -1
	for i, r := range results {
		name := r.Product.Name
		if name == "Budweiser" || name == "Bud Light" {
			lastBudIdx = i
		}
		if len(name) >= 5 && name[:5] == "Funky" && firstFunkyIdx == -1 {
			firstFunkyIdx = i
		}
	}

	if firstFunkyIdx != -1 && lastBudIdx > firstFunkyIdx {
		t.Errorf("Funky Buddha (index %d) appeared before last Bud* product (index %d)", firstFunkyIdx, lastBudIdx)
		for i, r := range results {
			t.Logf("  result[%d]: %s (score=%.4f)", i, r.Product.Name, r.Score)
		}
	}
}

func TestRecordSelectionMarksOnlySelectedCategoryDirty(t *testing.T) {
	products := []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
	}

	e := New(products)
	if len(e.dirtyCats) != 0 {
		t.Fatalf("expected clean category cache after init, got %+v", e.dirtyCats)
	}

	e.RecordSelection(0)

	if len(e.dirtyCats) != 1 {
		t.Fatalf("expected exactly one dirty category, got %+v", e.dirtyCats)
	}
	if _, ok := e.dirtyCats["beer"]; !ok {
		t.Fatalf("expected beer cache to be marked dirty, got %+v", e.dirtyCats)
	}
	if _, ok := e.dirtyCats["shoes"]; ok {
		t.Fatalf("did not expect shoes cache to be dirty, got %+v", e.dirtyCats)
	}

	if results := e.Search("shoes"); len(results) == 0 {
		t.Fatal("expected exact category results for shoes")
	}
	if _, ok := e.dirtyCats["beer"]; !ok {
		t.Fatal("expected beer cache to remain dirty after searching shoes")
	}

	if results := e.Search("beer"); len(results) == 0 {
		t.Fatal("expected exact category results for beer")
	}
	if _, ok := e.dirtyCats["beer"]; ok {
		t.Fatalf("expected beer cache to be refreshed, got %+v", e.dirtyCats)
	}
}
