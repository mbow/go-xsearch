package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProducts(t *testing.T) {
	t.Parallel()
	// Create a temp JSON file
	dir := t.TempDir()
	path := filepath.Join(dir, "products.json")
	data := `[
		{"name": "Budweiser", "category": "beer"},
		{"name": "Nike Air Max", "category": "shoes"}
	]`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	products, err := LoadProducts(path)
	if err != nil {
		t.Fatalf("LoadProducts() error: %v", err)
	}

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}

	if products[0].Name != "Budweiser" {
		t.Errorf("expected name 'Budweiser', got %q", products[0].Name)
	}
	if products[0].Category != "beer" {
		t.Errorf("expected category 'beer', got %q", products[0].Category)
	}
	if products[1].Name != "Nike Air Max" {
		t.Errorf("expected name 'Nike Air Max', got %q", products[1].Name)
	}
	if products[1].Category != "shoes" {
		t.Errorf("expected category 'shoes', got %q", products[1].Category)
	}
}

func TestLoadProductsFileNotFound(t *testing.T) {
	t.Parallel()
	_, err := LoadProducts("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoadProductsInvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadProducts(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExtractPrefixes(t *testing.T) {
	t.Parallel()
	products := []Product{
		{Name: "Budweiser", Category: "beer"},
		{Name: "Bud Light", Category: "beer"},
		{Name: "Nike Air Max", Category: "shoes"},
	}

	prefixes := ExtractPrefixes(products)
	if len(prefixes) == 0 {
		t.Fatal("expected prefixes")
	}

	has := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		has[p] = true
	}

	for _, want := range []string{"b", "bu", "n", "ni"} {
		if !has[want] {
			t.Errorf("missing expected prefix %q", want)
		}
	}

	seen := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		if _, ok := seen[p]; ok {
			t.Errorf("duplicate prefix %q", p)
		}
		seen[p] = struct{}{}
	}
}

func TestStableID(t *testing.T) {
	t.Parallel()
	id := StableID(Product{Name: "Bud Light", Category: "beer"})
	if id != "beer-bud-light" {
		t.Fatalf("unexpected stable id: %q", id)
	}
}
