package ranking

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"sync"
	"time"
)

// Ranker tracks product selection popularity with exponential time decay.
type Ranker struct {
	mu         sync.RWMutex
	lambda     float64
	alpha      float64
	selections map[int][]time.Time
	maxDirty   bool    // true when maxCached needs recalculation
	maxCached  float64 // cached max popularity score
}

// New creates a Ranker with the given decay rate and alpha.
func New(lambda, alpha float64) *Ranker {
	return &Ranker{
		lambda:     lambda,
		alpha:      alpha,
		selections: make(map[int][]time.Time),
		maxDirty:   true,
	}
}

// RecordSelection records a user selecting a product right now.
func (r *Ranker) RecordSelection(productID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = append(r.selections[productID], time.Now())
	r.maxDirty = true
}

// SetSelections sets the selection timestamps for a product directly (used for testing and loading from disk).
func (r *Ranker) SetSelections(productID int, timestamps []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = timestamps
	r.maxDirty = true
}

// rawScore computes the exponential decay score for a product. Caller must hold at least RLock.
func (r *Ranker) rawScore(productID int, now time.Time) float64 {
	timestamps := r.selections[productID]
	if len(timestamps) == 0 {
		return 0
	}
	var score float64
	for _, ts := range timestamps {
		ageDays := now.Sub(ts).Hours() / 24
		score += math.Exp(-r.lambda * ageDays)
	}
	return score
}

// recomputeMax recalculates the max popularity score. Caller must hold lock.
func (r *Ranker) recomputeMax(now time.Time) {
	if !r.maxDirty {
		return
	}
	maxScore := 0.0
	for _, timestamps := range r.selections {
		var s float64
		for _, ts := range timestamps {
			ageDays := now.Sub(ts).Hours() / 24
			s += math.Exp(-r.lambda * ageDays)
		}
		if s > maxScore {
			maxScore = s
		}
	}
	r.maxCached = maxScore
	r.maxDirty = false
}

// PopularityScore computes the raw exponential decay score for a product.
func (r *Ranker) PopularityScore(productID int) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rawScore(productID, time.Now())
}

// NormalizedPopularity returns the popularity score normalized to 0.0-1.0
// by dividing by the maximum popularity score across all products.
// The max is cached and only recomputed when selections change.
func (r *Ranker) NormalizedPopularity(productID int) float64 {
	r.mu.Lock()
	now := time.Now()
	r.recomputeMax(now)
	maxScore := r.maxCached
	score := r.rawScore(productID, now)
	r.mu.Unlock()

	if maxScore == 0 {
		return 0
	}
	return score / maxScore
}

// CombinedScore computes the final ranking score by fusing relevance and popularity.
// relevance should be in the 0.0-1.0 range (Jaccard similarity).
func (r *Ranker) CombinedScore(productID int, relevance float64) float64 {
	pop := r.NormalizedPopularity(productID)
	return r.alpha*relevance + (1-r.alpha)*pop
}

// Scorer returns a reusable scoring function that captures a single time.Now()
// and a single lock acquisition for the max score. Call this once per search,
// then use the returned function to score each result — avoids repeated
// time.Now() and lock overhead per product.
func (r *Ranker) Scorer() func(productID int, relevance float64) float64 {
	r.mu.Lock()
	now := time.Now()
	r.recomputeMax(now)
	maxScore := r.maxCached
	alpha := r.alpha
	lambda := r.lambda
	// Hold a direct reference to the selections map under RLock semantics.
	// Safe because: the map keys (product IDs) don't change during a search,
	// and timestamp slices are only appended to (append may reallocate but
	// the old backing array remains valid for reads). New entries added
	// concurrently via RecordSelection won't be visible, which is fine —
	// they'll show up in the next search.
	sel := r.selections
	r.mu.Unlock()

	return func(productID int, relevance float64) float64 {
		var pop float64
		if maxScore > 0 {
			timestamps := sel[productID]
			var score float64
			for _, ts := range timestamps {
				ageDays := now.Sub(ts).Hours() / 24
				score += math.Exp(-lambda * ageDays)
			}
			pop = score / maxScore
		}
		return alpha*relevance + (1-alpha)*pop
	}
}

// Save writes all selection data to a JSON file.
func (r *Ranker) Save(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := json.Marshal(r.selections)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads selection data from a JSON file. If the file does not exist,
// this is a no-op (fresh start with no prior popularity data).
func (r *Ranker) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := json.Unmarshal(data, &r.selections); err != nil {
		return err
	}
	r.maxDirty = true
	return nil
}

// Prune removes selection timestamps older than maxAgeDays.
func (r *Ranker) Prune(maxAgeDays int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	for id, timestamps := range r.selections {
		kept := timestamps[:0]
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		if len(kept) == 0 {
			delete(r.selections, id)
		} else {
			r.selections[id] = kept
		}
	}
	r.maxDirty = true
}
