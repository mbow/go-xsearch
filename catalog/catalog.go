// Package catalog defines the product data model and provides loading functions.
//
// Products can be loaded from JSON files at runtime via [LoadProducts], or from
// compile-time embedded CBOR data via [EmbeddedProducts] (see embed.go).
package catalog

import (
	"encoding/json"
	"fmt"
	"os"
)

// Product represents a single item in the product catalog.
type Product struct {
	Name     string   `json:"name" cbor:"name"`
	Category string   `json:"category" cbor:"category"`
	Tags     []string `json:"tags,omitempty" cbor:"tags,omitempty"`
}

// LoadProducts reads a JSON file at path and returns the parsed products.
func LoadProducts(path string) ([]Product, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var products []Product
	if err := json.Unmarshal(data, &products); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	return products, nil
}
