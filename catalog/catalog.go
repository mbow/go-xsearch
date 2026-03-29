package catalog

import (
	"encoding/json"
	"os"
)

// Product represents a single item in the product catalog.
type Product struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// LoadProducts reads a JSON file and returns a slice of products.
func LoadProducts(path string) ([]Product, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var products []Product
	if err := json.Unmarshal(data, &products); err != nil {
		return nil, err
	}

	return products, nil
}
