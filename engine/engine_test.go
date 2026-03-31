package engine

import (
	"search/catalog"
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
