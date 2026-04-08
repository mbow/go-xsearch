// Package catalog defines the product data model and source-data loading helpers.
//
// Products are loaded from JSON for snapshot generation. Runtime lookup uses the
// self-contained embedded xsearch snapshot exposed from embed.go.
package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mbow/go-xsearch/xsearch"
)

// Product represents a single item in the product catalog.
type Product struct {
	Name     string   `json:"name" cbor:"name"`
	Category string   `json:"category" cbor:"category"`
	Tags     []string `json:"tags,omitzero" cbor:"tags,omitempty"`
}

// SearchID returns the stable generated search ID for the product.
func (p Product) SearchID() string {
	return StableID(p)
}

// SearchFields returns the weighted searchable fields for the product.
func (p Product) SearchFields() []xsearch.Field {
	fields := []xsearch.Field{
		{Name: "name", Values: []string{p.Name}, Weight: 1.0},
		{Name: "category", Values: []string{p.Category}, Weight: 0.5},
	}
	if len(p.Tags) > 0 {
		fields = append(fields, xsearch.Field{
			Name:   "tags",
			Values: p.Tags,
			Weight: 0.4,
		})
	}
	return fields
}

// StableID derives a deterministic ID from category and name.
func StableID(p Product) string {
	return slugify(p.Category) + "-" + slugify(p.Name)
}

func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	lastDash := true
	for i := range len(s) {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z':
			b.WriteByte(c + ('a' - 'A'))
			lastDash = false
		case ('a' <= c && c <= 'z') || ('0' <= c && c <= '9'):
			b.WriteByte(c)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
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
