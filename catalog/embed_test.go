package catalog

import (
	"testing"

	"github.com/mbow/xsearch"
)

func TestEmbeddedSnapshot(t *testing.T) {
	t.Parallel()
	snapshot, err := EmbeddedSnapshot()
	if err != nil {
		t.Fatalf("EmbeddedSnapshot() error: %v", err)
	}
	if len(snapshot) == 0 {
		t.Fatal("expected embedded snapshot bytes")
	}
}

func TestEmbeddedSnapshotLoads(t *testing.T) {
	t.Parallel()
	snapshot, err := EmbeddedSnapshot()
	if err != nil {
		t.Fatalf("EmbeddedSnapshot() error: %v", err)
	}
	products, err := LoadProducts("../data/products.json")
	if err != nil {
		t.Fatalf("LoadProducts() error: %v", err)
	}
	engine, err := xsearch.NewFromSnapshot(snapshot, products)
	if err != nil {
		t.Fatalf("NewFromSnapshot() error: %v", err)
	}
	results := engine.Search("bud")
	if len(results) == 0 {
		t.Fatal("expected search results from embedded snapshot")
	}
}
