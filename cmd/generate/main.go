// cmd/generate/main.go reads data/products.json and produces catalog/data.cbor
// containing products, a pre-built bloom filter, and a pre-built n-gram index.
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

	"search/bloom"
	"search/catalog"
	"search/index"

	"github.com/fxamacker/cbor/v2"
)

// Payload is the full serialized dataset: products + pre-built index + bloom filter.
type Payload struct {
	Products  []catalog.Product `cbor:"products"`
	BloomSnap bloom.Snapshot    `cbor:"bloom"`
	IndexSnap index.Snapshot    `cbor:"index"`
}

const (
	bloomSize      = 20000
	bloomHashCount = 3
)

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
	// Read and parse source JSON
	jsonData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", inputPath, err)
	}

	var products []catalog.Product
	if err := json.Unmarshal(jsonData, &products); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	if len(products) == 0 {
		return fmt.Errorf("no products found in %s", inputPath)
	}

	// Build the n-gram index
	idx := index.NewIndex(products)
	indexSnap := idx.ToSnapshot()

	// Build the bloom filter
	bf := bloom.New(bloomSize, bloomHashCount)
	for _, p := range products {
		for _, g := range index.ExtractTrigrams(p.Name) {
			bf.Add(g)
		}
		for _, g := range index.ExtractTrigrams(p.Category) {
			bf.Add(g)
		}
	}
	bloomSnap := bf.ToSnapshot()

	// Build full payload
	payload := Payload{
		Products:  products,
		BloomSnap: bloomSnap,
		IndexSnap: indexSnap,
	}

	// Canonical CBOR for deterministic output
	em, err := cbor.EncOptions{
		Sort: cbor.SortCanonical,
	}.EncMode()
	if err != nil {
		return fmt.Errorf("creating CBOR encoder: %w", err)
	}

	cborData, err := em.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling to CBOR: %w", err)
	}

	if err := os.WriteFile(outputPath, cborData, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outputPath, err)
	}

	fmt.Printf("generated %s: %d products, %d bytes JSON -> %d bytes CBOR (%.0f%% smaller)\n",
		outputPath, len(products), len(jsonData), len(cborData),
		(1-float64(len(cborData))/float64(len(jsonData)))*100)
	fmt.Printf("  includes: pre-built bloom filter (%d bits) + n-gram index (%d posting lists)\n",
		bloomSnap.Size, len(indexSnap.Posting))

	return nil
}
