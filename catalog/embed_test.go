package catalog

import (
	"testing"
)

func TestEmbeddedProducts(t *testing.T) {
	products, err := EmbeddedProducts()
	if err != nil {
		t.Fatalf("EmbeddedProducts() error: %v", err)
	}

	if len(products) == 0 {
		t.Fatal("expected non-empty product list from embedded CBOR")
	}

	// Verify first product has populated fields
	if products[0].Name == "" {
		t.Error("expected first product to have a name")
	}
	if products[0].Category == "" {
		t.Error("expected first product to have a category")
	}
}

func TestEmbeddedCount(t *testing.T) {
	count, err := EmbeddedCount()
	if err != nil {
		t.Fatalf("EmbeddedCount() error: %v", err)
	}

	if count != 226 {
		t.Errorf("expected 226 embedded products, got %d", count)
	}
}

func TestGetByName(t *testing.T) {
	p, err := GetByName("Budweiser")
	if err != nil {
		t.Fatalf("GetByName() error: %v", err)
	}
	if p == nil {
		t.Fatal("expected to find Budweiser")
	}
	if p.Category != "beer" {
		t.Errorf("expected category 'beer', got %q", p.Category)
	}
}

func TestGetByNameNotFound(t *testing.T) {
	p, err := GetByName("Nonexistent Product")
	if err != nil {
		t.Fatalf("GetByName() error: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil for nonexistent product, got %+v", p)
	}
}

func TestGetByID(t *testing.T) {
	p, err := GetByID(0)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if p == nil {
		t.Fatal("expected to find product at ID 0")
	}
	if p.Name == "" {
		t.Error("expected product at ID 0 to have a name")
	}
}

func TestGetByIDNotFound(t *testing.T) {
	p, err := GetByID(99999)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil for invalid ID, got %+v", p)
	}
}

func TestEmbeddedMatchesJSON(t *testing.T) {
	// Verify CBOR embedded data matches the JSON source
	jsonProducts, err := LoadProducts("../data/products.json")
	if err != nil {
		t.Fatalf("LoadProducts() error: %v", err)
	}

	cborProducts, err := EmbeddedProducts()
	if err != nil {
		t.Fatalf("EmbeddedProducts() error: %v", err)
	}

	if len(jsonProducts) != len(cborProducts) {
		t.Fatalf("count mismatch: JSON=%d, CBOR=%d", len(jsonProducts), len(cborProducts))
	}

	for i := range jsonProducts {
		if jsonProducts[i].Name != cborProducts[i].Name {
			t.Errorf("product[%d] name mismatch: JSON=%q, CBOR=%q", i, jsonProducts[i].Name, cborProducts[i].Name)
		}
		if jsonProducts[i].Category != cborProducts[i].Category {
			t.Errorf("product[%d] category mismatch: JSON=%q, CBOR=%q", i, jsonProducts[i].Category, cborProducts[i].Category)
		}
	}
}
