package bm25

import (
	"slices"
	"testing"

	"github.com/mbow/go-xsearch/catalog"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		input string
		want []string
	}{
		{"simple", "Budweiser", []string{"budweiser"}},
		{"multi word", "Nike Air Max", []string{"nike", "air", "max"}},
		{"punctuation", "Ben & Jerry's", []string{"ben", "jerry's"}},
		{"extra spaces", "  hello   world  ", []string{"hello", "world"}},
		{"empty", "", nil},
		{"number", "No. 12 Light", []string{"no.", "12", "light"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func testProducts() []catalog.Product {
	return []catalog.Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Bud Light", Category: "beer"},
		{Name: "Funky Buddha Double Lambic", Category: "beer"},
		{Name: "Funky Buddha Cask Kolsch", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
		{Name: "Miller Lite", Category: "beer"},
	}
}

func TestNewIndex(t *testing.T) {
	idx := NewIndex(testProducts())

	// IDF exists for "budweiser"
	if _, ok := idx.idf["budweiser"]; !ok {
		t.Error("expected IDF entry for 'budweiser'")
	}

	// IDF("beer") < IDF("budweiser") — beer appears in 5/6 products
	if idx.idf["beer"] >= idx.idf["budweiser"] {
		t.Errorf("expected IDF(beer) < IDF(budweiser): %f >= %f",
			idx.idf["beer"], idx.idf["budweiser"])
	}

	// avgDocLen > 0
	if idx.avgDocLen <= 0 {
		t.Errorf("expected avgDocLen > 0, got %f", idx.avgDocLen)
	}

	// Posting list for "budweiser" contains product 0
	posted := idx.posting["budweiser"]
	if !slices.Contains(posted, 0) {
		t.Errorf("expected posting['budweiser'] to contain 0, got %v", posted)
	}

	// wordPrefixes[0] contains "bud" (Budweiser)
	if _, ok := idx.wordPrefixes[0]["bud"]; !ok {
		t.Error("expected wordPrefixes[0] to contain 'bud' (from Budweiser)")
	}

	// wordPrefixes[2] contains "bud" because "bud" is a prefix of "buddha"
	if _, ok := idx.wordPrefixes[2]["bud"]; !ok {
		t.Error("expected wordPrefixes[2] to contain 'bud' (prefix of 'buddha')")
	}

	// wordPrefixes[2] does NOT contain "budweiser" (no name word starts with "budweiser")
	if _, ok := idx.wordPrefixes[2]["budweiser"]; ok {
		t.Error("expected wordPrefixes[2] NOT to contain 'budweiser'")
	}
}

func TestScore(t *testing.T) {
	idx := NewIndex(testProducts())
	terms := Tokenize("budweiser")

	// Budweiser (product 0) should score positive for "budweiser"
	score0 := idx.Score(0, terms)
	if score0 <= 0 {
		t.Errorf("expected positive score for Budweiser, got %f", score0)
	}

	// Funky Buddha (product 2) should score 0 for "budweiser"
	score2 := idx.Score(2, terms)
	if score2 != 0 {
		t.Errorf("expected 0 score for Funky Buddha on 'budweiser', got %f", score2)
	}
}

func TestSearch(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("budweiser")

	if len(results) == 0 {
		t.Fatal("expected results for 'budweiser'")
	}
	if results[0].ProductID != 0 {
		t.Errorf("expected product 0 (Budweiser) first, got product %d", results[0].ProductID)
	}
}

func TestSearch_PrefixBoost(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("bud")

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results for 'bud', got %d", len(results))
	}

	// Budweiser and Bud Light should appear in the results with PrefixMatch
	topIDs := make(map[int]bool)
	for _, r := range results[:2] {
		topIDs[r.ProductID] = true
		if !r.PrefixMatch {
			t.Errorf("expected PrefixMatch=true for product %d", r.ProductID)
		}
	}
	if !topIDs[0] || !topIDs[1] {
		t.Errorf("expected products 0 (Budweiser) and 1 (Bud Light) in top 2, got %v", results[:2])
	}
}

func TestSearch_NoResults(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("xyzzyplugh")

	if len(results) != 0 {
		t.Errorf("expected no results for gibberish, got %d", len(results))
	}
}

func TestSearch_Empty(t *testing.T) {
	idx := NewIndex(testProducts())
	results := idx.Search("")

	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}
