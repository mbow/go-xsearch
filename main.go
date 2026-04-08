// Product search HTTP server entry point.
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
	"github.com/mbow/go-xsearch/internal/server"
	"github.com/mbow/go-xsearch/ranking"
	"github.com/mbow/go-xsearch/xsearch"
)

const snapshotPeriod = 60 * time.Second

func main() {
	dataDir := "data"

	products, err := catalog.LoadProducts(filepath.Join(dataDir, "products.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading products: %v\n", err)
		os.Exit(1)
	}

	snapshot, err := catalog.EmbeddedSnapshot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading embedded snapshot: %v\n", err)
		os.Exit(1)
	}

	ranker := ranking.New(0.05, 0.6)
	eng, err := xsearch.NewFromSnapshot(snapshot, products,
		xsearch.WithLimit(10),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating engine: %v\n", err)
		os.Exit(1)
	}
	ranker.SetIDs(eng.IDs())

	app := server.New(eng, ranker, 1024)
	app.LoadTemplates()

	popPath := filepath.Join(dataDir, "popularity.json")
	migrated, err := ranker.LoadWithMigration(popPath, eng.IDs())
	if err != nil {
		log.Printf("warning: could not load popularity data: %v", err)
	} else if migrated {
		if err := ranker.Save(popPath); err != nil {
			log.Printf("warning: could not rewrite migrated popularity data: %v", err)
		}
	}
	ranker.Prune(90)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		ticker := time.NewTicker(snapshotPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ranker.Save(popPath); err != nil {
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

	if err := ranker.Save(popPath); err != nil {
		log.Printf("error saving final popularity data: %v", err)
	}
	log.Println("server stopped")
}
