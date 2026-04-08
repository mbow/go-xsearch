package xsearch

import (
	"testing"
)

func TestExtractNormalizedTrigrams(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"ab", 0},
		{"abc", 1},
		{"budweiser", 7},
	}
	for _, tt := range tests {
		got := extractNormalizedTrigrams(nil, tt.input)
		if len(got) != tt.want {
			t.Errorf("extractNormalizedTrigrams(%q): got %d, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestNgramIndex_Search(t *testing.T) {
	items := []preparedItem{
		{ID: "budweiser", Fields: []internalField{{Name: "name", Values: []string{"budweiser"}, Weight: 1.0}}, primaryField: 0},
		{ID: "corona", Fields: []internalField{{Name: "name", Values: []string{"corona extra"}, Weight: 1.0}}, primaryField: 0},
	}
	idx := newNgramIndex(items)
	results := idx.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results for 'budweiser'")
	}
	if results[0].Doc != 0 {
		t.Fatalf("expected doc 0, got %d", results[0].Doc)
	}
}

func TestNgramIndex_PrefixSearch(t *testing.T) {
	items := []preparedItem{
		{ID: "bud", Fields: []internalField{{Name: "name", Values: []string{"budweiser"}, Weight: 1.0}}, primaryField: 0},
		{ID: "cor", Fields: []internalField{{Name: "name", Values: []string{"corona"}, Weight: 1.0}}, primaryField: 0},
	}
	idx := newNgramIndex(items)
	results := idx.Search("bu")
	if len(results) == 0 {
		t.Fatal("expected prefix match for 'bu'")
	}
	if results[0].Doc != 0 {
		t.Fatalf("expected doc 0 for prefix match, got %d", results[0].Doc)
	}
}

func TestNgramIndex_WeightedScoring(t *testing.T) {
	items := []preparedItem{
		{ID: "name-match", Fields: []internalField{
			{Name: "name", Values: []string{"budweiser lager"}, Weight: 1.0},
			{Name: "tags", Values: []string{"cold", "crisp"}, Weight: 0.1},
		}, primaryField: 0},
		{ID: "tag-match", Fields: []internalField{
			{Name: "name", Values: []string{"corona extra"}, Weight: 1.0},
			{Name: "tags", Values: []string{"budweiser style"}, Weight: 0.1},
		}, primaryField: 0},
	}
	idx := newNgramIndex(items)
	results := idx.SearchWithGrams(extractNormalizedTrigrams(nil, "budweiser"))
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Doc != 0 {
		t.Fatal("expected name-match (doc 0) to rank first due to higher field weight")
	}
	if results[0].Score <= results[1].Score {
		t.Fatal("expected name-match to have higher score")
	}
}

func TestFallbackIndex_BestGroup(t *testing.T) {
	items := []preparedItem{
		{ID: "a", Fields: []internalField{
			{Name: "name", Values: []string{"alpha"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}, primaryField: 0},
		{ID: "b", Fields: []internalField{
			{Name: "name", Values: []string{"beta"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}, primaryField: 0},
		{ID: "c", Fields: []internalField{
			{Name: "name", Values: []string{"gamma"}, Weight: 1.0},
			{Name: "category", Values: []string{"wine"}, Weight: 0.5},
		}, primaryField: 0},
	}
	idx := newFallbackIndex(items, "category")

	docs, ok := idx.exact("beer")
	if !ok {
		t.Fatal("expected exact match for 'beer'")
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs in beer group, got %d", len(docs))
	}

	beerGrams := extractNormalizedTrigrams(nil, "beer")
	docs, ok = idx.best("beer", beerGrams)
	if !ok {
		t.Fatal("expected best match for 'beer'")
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestFallbackIndex_Nil(t *testing.T) {
	idx := newFallbackIndex(nil, "")
	if idx != nil {
		t.Fatal("expected nil fallback for empty field name")
	}
	docs, ok := (*fallbackIndex)(nil).exact("anything")
	if ok || docs != nil {
		t.Fatal("nil fallback should return false")
	}
}
