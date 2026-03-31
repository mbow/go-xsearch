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
