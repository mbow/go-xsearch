package ranking

import (
	"math"
	"sync"
	"time"
)

// Ranker tracks product selection popularity with exponential time decay.
type Ranker struct {
	mu         sync.RWMutex
	lambda     float64
	alpha      float64
	selections map[int][]time.Time
}

// New creates a Ranker with the given decay rate and alpha.
func New(lambda, alpha float64) *Ranker {
	return &Ranker{
		lambda:     lambda,
		alpha:      alpha,
		selections: make(map[int][]time.Time),
	}
}

// RecordSelection records a user selecting a product right now.
func (r *Ranker) RecordSelection(productID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = append(r.selections[productID], time.Now())
}

// SetSelections sets the selection timestamps for a product directly (used for testing and loading from disk).
func (r *Ranker) SetSelections(productID int, timestamps []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = timestamps
}

// PopularityScore computes the raw exponential decay score for a product.
func (r *Ranker) PopularityScore(productID int) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	timestamps := r.selections[productID]
	if len(timestamps) == 0 {
		return 0
	}

	now := time.Now()
	var score float64
	for _, ts := range timestamps {
		ageDays := now.Sub(ts).Hours() / 24
		score += math.Exp(-r.lambda * ageDays)
	}
	return score
}

// NormalizedPopularity returns the popularity score normalized to 0.0-1.0
// by dividing by the maximum popularity score across all products.
func (r *Ranker) NormalizedPopularity(productID int) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	maxScore := 0.0
	now := time.Now()
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

	if maxScore == 0 {
		return 0
	}

	var score float64
	for _, ts := range r.selections[productID] {
		ageDays := now.Sub(ts).Hours() / 24
		score += math.Exp(-r.lambda * ageDays)
	}
	return score / maxScore
}
