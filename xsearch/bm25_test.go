package xsearch

import "testing"

func TestBM25Tokenize(t *testing.T) {
	tokens := tokenize("Hello World! 123")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "hello" {
		t.Fatalf("expected 'hello', got %q", tokens[0])
	}
}

func TestBM25Index_Search(t *testing.T) {
	items := []preparedItem{
		{ID: "budweiser", Fields: []internalField{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}, primaryField: 0},
		{ID: "corona", Fields: []internalField{
			{Name: "name", Values: []string{"Corona Extra"}, Weight: 1.0},
			{Name: "category", Values: []string{"beer"}, Weight: 0.5},
		}, primaryField: 0},
	}
	cfg := defaultConfig()
	idx := newBM25Index(items, cfg)
	results := idx.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results for 'budweiser'")
	}
	if results[0].Doc != 0 {
		t.Fatalf("expected doc 0, got %d", results[0].Doc)
	}
}

func TestBM25Index_PerFieldWeighting(t *testing.T) {
	items := []preparedItem{
		{ID: "name-match", Fields: []internalField{
			{Name: "name", Values: []string{"budweiser lager"}, Weight: 1.0},
			{Name: "tags", Values: []string{"american"}, Weight: 0.1},
		}, primaryField: 0},
		{ID: "tag-match", Fields: []internalField{
			{Name: "name", Values: []string{"corona extra"}, Weight: 1.0},
			{Name: "tags", Values: []string{"budweiser style"}, Weight: 0.1},
		}, primaryField: 0},
	}
	cfg := defaultConfig()
	idx := newBM25Index(items, cfg)
	results := idx.Search("budweiser")
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Doc != 0 {
		t.Fatal("expected name-match (doc 0) to rank first")
	}
	if results[0].Score <= results[1].Score {
		t.Fatal("expected name-match to have higher score")
	}
}

func TestBM25Index_PrefixBoost(t *testing.T) {
	items := []preparedItem{
		{ID: "bud-direct", Fields: []internalField{
			{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0},
		}, primaryField: 0},
		{ID: "bud-mention", Fields: []internalField{
			{Name: "name", Values: []string{"Funky Buddha Floridian"}, Weight: 1.0},
		}, primaryField: 0},
	}
	cfg := defaultConfig()
	idx := newBM25Index(items, cfg)
	results := idx.Search("bud")
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Doc != 0 {
		t.Fatal("expected Budweiser (doc 0) to rank first due to prefix boost")
	}
}

func TestBM25Index_MultiValueField(t *testing.T) {
	items := []preparedItem{
		{ID: "drink", Fields: []internalField{
			{Name: "name", Values: []string{"Margarita"}, Weight: 1.0},
			{Name: "ingredients", Values: []string{"tequila", "lime", "triple sec"}, Weight: 0.4},
		}, primaryField: 0},
	}
	cfg := defaultConfig()
	idx := newBM25Index(items, cfg)
	results := idx.Search("tequila")
	if len(results) == 0 {
		t.Fatal("expected match on ingredient")
	}
}

func TestBM25Index_ScoreDoc(t *testing.T) {
	items := []preparedItem{
		{ID: "a", Fields: []internalField{
			{Name: "name", Values: []string{"budweiser lager beer"}, Weight: 1.0},
		}, primaryField: 0},
	}
	cfg := defaultConfig()
	idx := newBM25Index(items, cfg)

	fi := idx.fields["name"]
	score := fi.scoreDoc(0, []string{"budweiser"}, cfg.bm25K1, cfg.bm25B)
	if score <= 0 {
		t.Fatalf("expected positive score, got %f", score)
	}

	score2 := fi.scoreDoc(0, []string{"nonexistent"}, cfg.bm25K1, cfg.bm25B)
	if score2 != 0 {
		t.Fatalf("expected 0 for missing term, got %f", score2)
	}
}
