// Package ranking implements time-decayed popularity scoring for search results.
//
// Each product's popularity is computed as the sum of exponentially decayed
// selection timestamps: score = Σ e^(-λ × age_in_days). This naturally handles
// both frequency (more selections = higher sum) and recency (older selections
// contribute less).
//
// The [Ranker.Scorer] method returns a closure that captures a single time.Now()
// and lock acquisition for an entire search operation, avoiding per-result overhead.
package ranking

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"sync"
	"time"
)

// Ranker tracks product selection popularity with exponential time decay.
type Ranker struct {
	mu            sync.RWMutex
	lambda        float64
	alpha         float64
	selections    map[int][]time.Time
	snapshotDirty bool
	snapshot      popularitySnapshot
}

type popularitySnapshot struct {
	normalized map[int]float64
}

// New creates a Ranker with the given decay rate and alpha.
func New(lambda, alpha float64) *Ranker {
	return &Ranker{
		lambda:        lambda,
		alpha:         alpha,
		selections:    make(map[int][]time.Time),
		snapshotDirty: true,
	}
}

// RecordSelection records a user selecting a product right now.
func (r *Ranker) RecordSelection(productID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = append(r.selections[productID], time.Now())
	r.snapshotDirty = true
}

// SetSelections sets the selection timestamps for a product directly (used for testing and loading from disk).
func (r *Ranker) SetSelections(productID int, timestamps []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[productID] = timestamps
	r.snapshotDirty = true
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

// rebuildSnapshotLocked recalculates normalized popularity scores. Caller must hold lock.
func (r *Ranker) rebuildSnapshotLocked() {
	if !r.snapshotDirty {
		return
	}

	if len(r.selections) == 0 {
		r.snapshot = popularitySnapshot{}
		r.snapshotDirty = false
		return
	}

	now := time.Now()
	normalized := make(map[int]float64, len(r.selections))
	maxScore := 0.0
	for id, timestamps := range r.selections {
		var s float64
		for _, ts := range timestamps {
			ageDays := now.Sub(ts).Hours() / 24
			s += math.Exp(-r.lambda * ageDays)
		}
		if s == 0 {
			continue
		}
		normalized[id] = s
		if s > maxScore {
			maxScore = s
		}
	}

	if maxScore > 0 {
		for id, s := range normalized {
			normalized[id] = s / maxScore
		}
	} else {
		normalized = nil
	}

	r.snapshot = popularitySnapshot{normalized: normalized}
	r.snapshotDirty = false
}

func (r *Ranker) snapshotView() (popularitySnapshot, float64) {
	r.mu.RLock()
	if !r.snapshotDirty {
		snapshot := r.snapshot
		alpha := r.alpha
		r.mu.RUnlock()
		return snapshot, alpha
	}
	r.mu.RUnlock()

	r.mu.Lock()
	r.rebuildSnapshotLocked()
	snapshot := r.snapshot
	alpha := r.alpha
	r.mu.Unlock()
	return snapshot, alpha
}

func (s popularitySnapshot) score(productID int) float64 {
	if s.normalized == nil {
		return 0
	}
	return s.normalized[productID]
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
	snapshot, _ := r.snapshotView()
	return snapshot.score(productID)
}

// CombinedScore computes the final ranking score by fusing relevance and popularity.
// relevance should be in the 0.0-1.0 range (Jaccard similarity).
func (r *Ranker) CombinedScore(productID int, relevance float64) float64 {
	pop := r.NormalizedPopularity(productID)
	return r.alpha*relevance + (1-r.alpha)*pop
}

// Scorer returns a reusable scoring function that captures a single time.Now()
// and a snapshot of the current popularity state. Call this once per search,
// then use the returned function to score each result — avoids repeated
// time.Now() and lock overhead per product.
//
// The snapshot is a pre-computed map of product ID to raw decay score, taken
// under the lock, so the returned closure is safe for concurrent use.
func (r *Ranker) Scorer() func(productID int, relevance float64) float64 {
	snapshot, alpha := r.snapshotView()

	return func(productID int, relevance float64) float64 {
		pop := snapshot.score(productID)
		return alpha*relevance + (1-alpha)*pop
	}
}

// Save writes all selection data to a JSON file at path.
func (r *Ranker) Save(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := json.Marshal(r.selections)
	if err != nil {
		return fmt.Errorf("ranking: marshaling selections: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("ranking: writing %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("ranking: atomically renaming %s: %w", path, err)
	}
	return nil
}

// Load reads selection data from a JSON file at path. If the file does not
// exist, this is a no-op (fresh start with no prior popularity data).
func (r *Ranker) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("ranking: reading %s: %w", path, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := json.Unmarshal(data, &r.selections); err != nil {
		return fmt.Errorf("ranking: unmarshaling selections: %w", err)
	}
	r.snapshotDirty = true
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
	r.snapshotDirty = true
}
