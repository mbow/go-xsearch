// cmd/generate/main.go reads data/products.json and produces catalog/data.cbor
// containing a self-contained xsearch snapshot.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/xsearch"
)

func main() {
	inputPath := flag.String("input", "data/products.json", "path to source JSON product catalog")
	outputPath := flag.String("output", "catalog/data.cbor", "path to write snapshot output")
	flag.Parse()

	if err := run(*inputPath, *outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
}

func run(inputPath, outputPath string) error {
	products, err := catalog.LoadProducts(inputPath)
	if err != nil {
		return err
	}
	if len(products) == 0 {
		return fmt.Errorf("no products found in %s", inputPath)
	}

	engine, err := xsearch.New(products,
		xsearch.WithBloom(100),
		xsearch.WithFallbackField("category"),
		xsearch.WithLimit(10),
	)
	if err != nil {
		return fmt.Errorf("building xsearch snapshot: %w", err)
	}

	snapshot, err := engine.Snapshot()
	if err != nil {
		return fmt.Errorf("serializing snapshot: %w", err)
	}

	if err := os.WriteFile(outputPath, snapshot, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outputPath, err)
	}

	fmt.Printf("generated %s: %d products, %d bytes\n", outputPath, len(products), len(snapshot))
	return nil
}
