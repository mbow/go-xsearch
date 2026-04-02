// Package ranking implements time-decayed popularity scoring for search results.
//
// Each product's popularity is computed as the sum of exponentially decayed
// selection timestamps: score = Σ e^(-λ × age_in_days). This naturally handles
// both frequency (more selections = higher sum) and recency (older selections
// contribute less).
//
// The [Ranker.ScoreView] method returns a lightweight immutable scorer for a
// single search operation, avoiding repeated lock work on the hot path.
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
	numProducts   int
	selections    map[int][]time.Time
	snapshotDirty bool
	snapshot      popularitySnapshot
}

type popularitySnapshot struct {
	scores []float64 // indexed by product ID, normalized to 0.0-1.0
}

// ScoreView is an immutable view of the current popularity snapshot.
// It is cheap to copy and safe to use concurrently.
type ScoreView struct {
	scores []float64
	alpha  float64
}

// New creates a Ranker with the given decay rate, alpha, and product count.
func New(lambda, alpha float64, numProducts int) *Ranker {
	return &Ranker{
		lambda:        lambda,
		alpha:         alpha,
		numProducts:   numProducts,
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
	scores := make([]float64, r.numProducts)
	maxScore := 0.0
	for id, timestamps := range r.selections {
		if id < 0 || id >= r.numProducts {
			continue
		}
		var s float64
		for _, ts := range timestamps {
			ageDays := now.Sub(ts).Hours() / 24
			s += math.Exp(-r.lambda * ageDays)
		}
		scores[id] = s
		if s > maxScore {
			maxScore = s
		}
	}

	if maxScore > 0 {
		for i, s := range scores {
			if s > 0 {
				scores[i] = s / maxScore
			}
		}
	} else {
		scores = nil
	}

	r.snapshot = popularitySnapshot{scores: scores}
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
	if productID < 0 || productID >= len(s.scores) {
		return 0
	}
	return s.scores[productID]
}

// Popularity returns the normalized popularity score for productID.
func (s ScoreView) Popularity(productID int) float64 {
	if productID < 0 || productID >= len(s.scores) {
		return 0
	}
	return s.scores[productID]
}

// Score blends relevance with popularity using the ranker's configured alpha.
func (s ScoreView) Score(productID int, relevance float64) float64 {
	pop := s.Popularity(productID)
	return s.alpha*relevance + (1-s.alpha)*pop
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
	return r.ScoreView().Popularity(productID)
}

// CombinedScore computes the final ranking score by fusing relevance and popularity.
// relevance should be in the 0.0-1.0 range (Jaccard similarity).
func (r *Ranker) CombinedScore(productID int, relevance float64) float64 {
	return r.ScoreView().Score(productID, relevance)
}

// ScoreView returns an immutable scorer for the current popularity snapshot.
func (r *Ranker) ScoreView() ScoreView {
	snapshot, alpha := r.snapshotView()
	return ScoreView{
		scores: snapshot.scores,
		alpha:  alpha,
	}
}

// Scorer returns the legacy closure-based scoring API.
func (r *Ranker) Scorer() func(productID int, relevance float64) float64 {
	view := r.ScoreView()
	return func(productID int, relevance float64) float64 {
		return view.Score(productID, relevance)
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
