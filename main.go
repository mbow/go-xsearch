// Product search HTTP server entry point.
//
// Usage:
//
//	go run .
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mbow/go-xsearch/catalog"
	"github.com/mbow/go-xsearch/engine"
	"github.com/mbow/go-xsearch/internal/server"
)

const snapshotPeriod = 60 * time.Second

func main() {
	dataDir := "data"

	products, err := catalog.EmbeddedProducts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded products: %v\n", err)
		os.Exit(1)
	}

	bloomRaw, err := catalog.EmbeddedBloomRaw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded bloom: %v\n", err)
		os.Exit(1)
	}

	indexRaw, err := catalog.EmbeddedIndexRaw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded index: %v\n", err)
		os.Exit(1)
	}

	bm25Raw, err := catalog.EmbeddedBM25Raw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded bm25: %v\n", err)
		os.Exit(1)
	}

	eng, err := engine.NewFromEmbedded(products, bloomRaw, indexRaw, bm25Raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating engine: %v\n", err)
		os.Exit(1)
	}

	app := server.New(eng, 1024)
	app.LoadTemplates()

	// Load popularity data if it exists
	popPath := filepath.Join(dataDir, "popularity.json")
	if err := app.Engine.Ranker().Load(popPath); err != nil {
		log.Printf("warning: could not load popularity data: %v", err)
	}

	// Prune old data and start periodic snapshots
	app.Engine.Ranker().Prune(90)
	go func() {
		ticker := time.NewTicker(snapshotPeriod)
		defer ticker.Stop()
		for range ticker.C {
			if err := app.Engine.Ranker().Save(popPath); err != nil {
				log.Printf("error saving popularity data: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()
	app.Routes(mux)

	addr := ":8080"
	log.Printf("starting server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
