// Product search HTTP server entry point.
//
// Usage:
//
//	go run .
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		ticker := time.NewTicker(snapshotPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := app.Engine.Ranker().Save(popPath); err != nil {
					log.Printf("error saving popularity data: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	mux := http.NewServeMux()
	app.Routes(mux)

	addr := cmp.Or(os.Getenv("LISTEN_ADDR"), "127.0.0.1:8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	log.Printf("starting server on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

	// Final save of popularity data before exit.
	if err := app.Engine.Ranker().Save(popPath); err != nil {
		log.Printf("error saving final popularity data: %v", err)
	}
	log.Println("server stopped")
}
