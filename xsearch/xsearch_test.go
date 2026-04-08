package xsearch

import (
	"math"
	"testing"
)

type testItem struct {
	id     string
	fields []Field
}

func (t testItem) SearchID() string      { return t.id }
func (t testItem) SearchFields() []Field { return t.fields }

type mapScorer map[int]float64

func (m mapScorer) Score(docIndex int) float64 { return m[docIndex] }

func testItems() []testItem {
	return []testItem{
		{id: "budweiser", fields: []Field{{Name: "name", Values: []string{"Budweiser"}, Weight: 1.0}, {Name: "category", Values: []string{"beer"}, Weight: 0.5}}},
		{id: "bud-light", fields: []Field{{Name: "name", Values: []string{"Bud Light"}, Weight: 1.0}, {Name: "category", Values: []string{"beer"}, Weight: 0.5}}},
		{id: "funky-buddha", fields: []Field{{Name: "name", Values: []string{"Funky Buddha Double Lambic"}, Weight: 1.0}, {Name: "category", Values: []string{"beer"}, Weight: 0.5}}},
		{id: "nike-air-max", fields: []Field{{Name: "name", Values: []string{"Nike Air Max"}, Weight: 1.0}, {Name: "category", Values: []string{"shoes"}, Weight: 0.5}}},
		{id: "nike-dunk", fields: []Field{{Name: "name", Values: []string{"Nike Dunk Low"}, Weight: 1.0}, {Name: "category", Values: []string{"shoes"}, Weight: 0.5}}},
		{id: "smoky-scotch", fields: []Field{{Name: "name", Values: []string{"Smoky Scotch"}, Weight: 1.0}, {Name: "category", Values: []string{"spirits"}, Weight: 0.5}, {Name: "tags", Values: []string{"peaty", "smoky"}, Weight: 0.4}}},
	}
}

func TestNewRejectsDuplicateIDs(t *testing.T) {
	_, err := New([]testItem{
		{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 1}}},
		{id: "a", fields: []Field{{Name: "name", Values: []string{"beta"}, Weight: 1}}},
	})
	if err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
}

func TestNewRejectsInvalidFields(t *testing.T) {
	_, err := New([]testItem{{id: "a", fields: []Field{{Name: "", Values: []string{"alpha"}, Weight: 1}}}})
	if err == nil {
		t.Fatal("expected error for empty field name")
	}

	_, err = New([]testItem{{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 0}}}})
	if err == nil {
		t.Fatal("expected error for non-positive weight")
	}

	_, err = New([]testItem{{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 1}, {Name: "name", Values: []string{"beta"}, Weight: 1}}}})
	if err == nil {
		t.Fatal("expected error for duplicate field names")
	}
}

func TestWithLimitValidation(t *testing.T) {
	items := []testItem{{id: "a", fields: []Field{{Name: "name", Values: []string{"alpha"}, Weight: 1}}}}
	if _, err := New(items, WithLimit(1)); err == nil {
		t.Fatal("expected error for limit < 2")
	}
	if _, err := New(items, WithLimit(101)); err == nil {
		t.Fatal("expected error for limit > 100")
	}
}

func TestSearchDirectMatch(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Search("nike")
	if len(results) == 0 {
		t.Fatal("expected results for nike")
	}
	if results[0].MatchType != MatchDirect {
		t.Fatalf("expected direct result, got %v", results[0].MatchType)
	}
}

func TestSearchFuzzyMatch(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Search("budwiser")
	if len(results) == 0 || results[0].ID != "budweiser" {
		t.Fatalf("expected budweiser result, got %+v", results)
	}
}

func TestSearchFallbackExactGroup(t *testing.T) {
	engine, err := New(testItems(), WithFallbackField("category"))
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Search("beer")
	if len(results) == 0 {
		t.Fatal("expected fallback results")
	}
	for _, result := range results {
		if result.MatchType != MatchFallback {
			t.Fatalf("expected fallback result, got %v", result.MatchType)
		}
	}
}

func TestSearchSortedByScore(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Search("nike", WithScoring(mapScorer{4: 10}))
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Fatalf("results not sorted at %d", i)
		}
	}
}

func TestMultiValueHighlightValueIndex(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Search("smoky")
	if len(results) == 0 {
		t.Fatal("expected smoky result")
	}
	highlights := results[0].Highlights["tags"]
	if len(highlights) == 0 {
		t.Fatal("expected tag highlight")
	}
	if highlights[0].ValueIndex != 1 {
		t.Fatalf("expected value index 1, got %d", highlights[0].ValueIndex)
	}
}

func TestGetAndIDs(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	if ids := engine.IDs(); len(ids) != len(testItems()) || ids[0] != "budweiser" {
		t.Fatalf("unexpected ids: %v", ids)
	}
	item, ok := engine.Get("nike-air-max")
	if !ok {
		t.Fatal("expected item lookup")
	}
	if item.ID != "nike-air-max" || item.Fields[0].Values[0] != "Nike Air Max" {
		t.Fatalf("unexpected item: %+v", item)
	}
	item.Fields[0].Values[0] = "mutated"
	item2, _ := engine.Get("nike-air-max")
	if item2.Fields[0].Values[0] != "Nike Air Max" {
		t.Fatal("Get should return a copy")
	}
}

func TestScorerClamping(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	results := engine.Search("bud", WithScoring(mapScorer{
		0: -1,
		1: math.Inf(1),
		2: math.NaN(),
	}))
	if len(results) == 0 {
		t.Fatal("expected results")
	}
}

func TestSearchWithBloom(t *testing.T) {
	eng, err := New(testItems(), WithBloom(100))
	if err != nil {
		t.Fatal(err)
	}
	results := eng.Search("budweiser")
	if len(results) == 0 {
		t.Fatal("expected results with bloom enabled")
	}
	if results[0].ID != "budweiser" {
		t.Fatalf("expected budweiser, got %q", results[0].ID)
	}
}

func TestSearchBloomRejectsNonsense(t *testing.T) {
	eng, err := New(testItems(), WithBloom(100))
	if err != nil {
		t.Fatal(err)
	}
	results := eng.Search("xzqwvp")
	if len(results) != 0 {
		t.Fatalf("expected bloom to reject nonsense, got %d results", len(results))
	}
}

func TestSearchWithoutBloomMatchesWithBloom(t *testing.T) {
	items := testItems()
	engNo, _ := New(items)
	engYes, _ := New(items, WithBloom(100))

	queries := []string{"budweiser", "nike", "bud"}
	for _, q := range queries {
		r1 := engNo.Search(q)
		r2 := engYes.Search(q)
		if len(r1) != len(r2) {
			t.Fatalf("query %q: result count differs: %d vs %d (bloom)", q, len(r1), len(r2))
		}
		for i := range r1 {
			if r1[i].ID != r2[i].ID {
				t.Fatalf("query %q result %d: ID %q vs %q (bloom)", q, i, r1[i].ID, r2[i].ID)
			}
		}
	}
}

func TestNewFromSnapshotRejectsBuildOptions(t *testing.T) {
	items := testItems()
	engine, err := New(items)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFromSnapshot(snapshot, items, WithBloom(10)); err == nil {
		t.Fatal("expected build-time option rejection")
	}
}

