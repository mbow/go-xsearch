package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"search/catalog"
	"search/engine"
)

// App holds application state.
type App struct {
	engine     *engine.Engine
	indexTmpl  *template.Template
	resultTmpl *template.Template
	dataDir    string
}

// ResultsData is the template data for search results.
type ResultsData struct {
	Query           string
	DirectResults   []engine.Result
	FallbackResults []engine.Result
}

func (app *App) loadTemplates() {
	app.indexTmpl = template.Must(template.ParseFiles("templates/index.html"))
	app.resultTmpl = template.Must(template.ParseFiles("templates/results.html"))
}

func (app *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	app.indexTmpl.Execute(w, nil)
}

func (app *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	results := app.engine.Search(query)

	data := ResultsData{Query: query}
	for _, res := range results {
		if res.MatchType == engine.MatchDirect {
			data.DirectResults = append(data.DirectResults, res)
		} else {
			data.FallbackResults = append(data.FallbackResults, res)
		}
	}

	app.resultTmpl.Execute(w, data)
}

func (app *App) handleSelect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(req.ID)
	if err != nil {
		http.Error(w, "invalid product ID", http.StatusBadRequest)
		return
	}

	app.engine.RecordSelection(id)
	w.WriteHeader(http.StatusOK)
}

func (app *App) startSnapshots(interval time.Duration) {
	path := filepath.Join(app.dataDir, "popularity.json")
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := app.engine.Ranker().Save(path); err != nil {
				log.Printf("error saving popularity data: %v", err)
			}
		}
	}()
}

func main() {
	dataDir := "data"

	// Load products
	products, err := catalog.LoadProducts(filepath.Join(dataDir, "products.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading products: %v\n", err)
		os.Exit(1)
	}

	app := &App{
		engine:  engine.New(products),
		dataDir: dataDir,
	}
	app.loadTemplates()

	// Load popularity data if it exists
	popPath := filepath.Join(dataDir, "popularity.json")
	if err := app.engine.Ranker().Load(popPath); err != nil {
		log.Printf("warning: could not load popularity data: %v", err)
	}

	// Prune old data and start periodic snapshots
	app.engine.Ranker().Prune(90)
	app.startSnapshots(60 * time.Second)

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("GET /search", app.handleSearch)
	mux.HandleFunc("POST /select", app.handleSelect)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	addr := ":8080"
	log.Printf("starting server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
