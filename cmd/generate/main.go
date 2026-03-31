// cmd/generate/main.go reads data/products.json and produces catalog/data.cbor.
//
// Usage:
//
//	go run cmd/generate/main.go
//	go run cmd/generate/main.go -input data/products.json -output catalog/data.cbor
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/fxamacker/cbor/v2"
)

// Product mirrors catalog.Product but lives here to keep the generator self-contained.
type Product struct {
	Name     string `json:"name" cbor:"name"`
	Category string `json:"category" cbor:"category"`
}

func main() {
	inputPath := flag.String("input", "data/products.json", "path to source JSON product catalog")
	outputPath := flag.String("output", "catalog/data.cbor", "path to write CBOR output")
	flag.Parse()

	if err := run(*inputPath, *outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
}

func run(inputPath, outputPath string) error {
	// Read source JSON
	jsonData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", inputPath, err)
	}

	// Parse JSON
	var products []Product
	if err := json.Unmarshal(jsonData, &products); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	if len(products) == 0 {
		return fmt.Errorf("no products found in %s", inputPath)
	}

	// Use canonical CBOR encoding for deterministic output
	em, err := cbor.EncOptions{
		Sort: cbor.SortCanonical,
	}.EncMode()
	if err != nil {
		return fmt.Errorf("creating CBOR encoder: %w", err)
	}

	// Marshal to CBOR
	cborData, err := em.Marshal(products)
	if err != nil {
		return fmt.Errorf("marshaling to CBOR: %w", err)
	}

	// Write output
	if err := os.WriteFile(outputPath, cborData, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outputPath, err)
	}

	fmt.Printf("generated %s: %d products, %d bytes JSON -> %d bytes CBOR (%.0f%% smaller)\n",
		outputPath, len(products), len(jsonData), len(cborData),
		(1-float64(len(cborData))/float64(len(jsonData)))*100)

	return nil
}
