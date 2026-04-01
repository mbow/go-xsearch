package index

import (
	"github.com/mbow/go-xsearch/catalog"
	"slices"
	"testing"
)

func TestExtractTrigrams(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"shoes", []string{"hoe", "oes", "sho"}},
		{"hi", nil},
		{"a", nil},
		{"", nil},
		{"abc", []string{"abc"}},
		{"SHOES", []string{"hoe", "oes", "sho"}},
		{"  Nike  ", []string{"ike", "nik"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractTrigrams(tt.input)
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("ExtractTrigrams(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func testProducts() []catalog.Product {
	return []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Miller Lite", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
		{Name: "Nike Dunk Low", Category: "shoes"},
		{Name: "AirPods Pro", Category: "headphones"},
	}
}

func TestNewIndex(t *testing.T) {
	idx := NewIndex(testProducts())
	if idx == nil {
		t.Fatal("NewIndex returned nil")
	}
}

func TestSearchPrefix(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("nik")
	if len(results) == 0 {
		t.Fatal("expected results for 'nik'")
	}

	foundNikeAir := false
	foundNikeDunk := false
	for _, r := range results {
		if r.ProductID == 2 {
			foundNikeAir = true
		}
		if r.ProductID == 3 {
			foundNikeDunk = true
		}
	}
	if !foundNikeAir {
		t.Error("expected to find Nike Air Max (ID 2)")
	}
	if !foundNikeDunk {
		t.Error("expected to find Nike Dunk Low (ID 3)")
	}
}

func TestSearchSubstring(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("pod")
	found := false
	for _, r := range results {
		if r.ProductID == 4 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find AirPods Pro (ID 4) searching for 'pod'")
	}
}

func TestSearchFuzzy(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("budwiser")
	found := false
	for _, r := range results {
		if r.ProductID == 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Budweiser (ID 0) with fuzzy search 'budwiser'")
	}
}

func TestSearchShortQuery(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("ni")
	found := false
	for _, r := range results {
		if r.ProductID == 2 || r.ProductID == 3 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Nike products with short query 'ni'")
	}
}

func TestSearchEmpty(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("")
	if len(results) != 0 {
		t.Errorf("expected no results for empty query, got %d", len(results))
	}
}

func TestSearchResultScore(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("budweiser")
	for _, r := range results {
		if r.ProductID == 0 {
			if r.Score < 0.5 {
				t.Errorf("exact match for Budweiser should have high score, got %f", r.Score)
			}
			return
		}
	}
	t.Error("expected to find Budweiser in results")
}

func TestSearchCategories(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.SearchCategories("bee")
	if len(results) == 0 {
		t.Fatal("expected category results for 'bee'")
	}
	if results[0] != "beer" {
		t.Errorf("expected best category 'beer', got %q", results[0])
	}
}

func TestSearchCategoriesNoMatch(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.SearchCategories("zzz")
	if len(results) != 0 {
		t.Errorf("expected no category results for 'zzz', got %v", results)
	}
}

func TestProductsByCategory(t *testing.T) {
	idx := NewIndex(testProducts())
	ids := idx.ProductsByCategory("beer")
	if len(ids) != 2 {
		t.Errorf("expected 2 beer products, got %d", len(ids))
	}
}

func TestPrefixSearchBinarySearch(t *testing.T) {
	products := []catalog.Product{
		{Name: "Apple Juice", Category: "drinks"},
		{Name: "Banana Split", Category: "dessert"},
		{Name: "Blueberry Muffin", Category: "bakery"},
		{Name: "Cherry Pie", Category: "bakery"},
	}
	idx := NewIndex(products)

	results := idx.Search("bl")
	found := false
	for _, r := range results {
		if r.ProductID == 2 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Blueberry Muffin for prefix 'bl'")
	}

	results = idx.Search("b")
	bCount := 0
	for _, r := range results {
		if r.ProductID == 1 || r.ProductID == 2 {
			bCount++
		}
	}
	if bCount < 2 {
		t.Errorf("expected Banana and Blueberry for prefix 'b', found %d", bCount)
	}
}

func TestSearchSaturationSafety(t *testing.T) {
	longName := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
	products := []catalog.Product{
		{Name: longName, Category: "test"},
	}
	idx := NewIndex(products)
	results := idx.Search(longName[:30])
	if len(results) == 0 {
		t.Fatal("expected results for long query substring")
	}
}

func TestSearchDeduplicatesRepeatedTrigrams(t *testing.T) {
	products := []catalog.Product{
		{Name: "Banana", Category: "fruit"},
	}
	idx := NewIndex(products)

	results := idx.Search("ana")
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d: %+v", len(results), results)
	}
	if results[0].ProductID != 0 {
		t.Fatalf("expected Banana result, got product %d", results[0].ProductID)
	}

	const want = 1.0 / 3.0
	if diff := results[0].Score - want; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("expected score %.6f, got %.6f", want, results[0].Score)
	}
}
