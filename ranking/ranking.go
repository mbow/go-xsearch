// Package ranking implements time-decayed popularity scoring for search results.
package ranking

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"strconv"
	"sync"
	"time"
)

// Ranker tracks item selection popularity with exponential time decay.
type Ranker struct {
	mu            sync.RWMutex
	lambda        float64
	alpha         float64
	orderedIDs    []string
	selections    map[string][]time.Time
	snapshotDirty bool
	snapshot      popularitySnapshot
}

type popularitySnapshot struct {
	scores []float64 // normalized to 0.0-1.0
}

// ScoreView is an immutable view of the current popularity snapshot.
type ScoreView struct {
	scores []float64
	alpha  float64
}

// New creates a Ranker with the given decay rate and alpha.
func New(lambda, alpha float64) *Ranker {
	return &Ranker{
		lambda:        lambda,
		alpha:         alpha,
		selections:    make(map[string][]time.Time),
		snapshotDirty: true,
	}
}

// SetIDs sets the ordered list of IDs to align with the search engine slice.
func (r *Ranker) SetIDs(orderedIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orderedIDs = append([]string(nil), orderedIDs...)
	r.snapshotDirty = true
}

// RecordSelection records a user selecting an item right now.
func (r *Ranker) RecordSelection(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[id] = append(r.selections[id], time.Now())
	r.snapshotDirty = true
}

// SetSelections sets timestamps directly for testing or migration.
func (r *Ranker) SetSelections(id string, timestamps []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selections[id] = slicesCloneTimes(timestamps)
	r.snapshotDirty = true
}

func slicesCloneTimes(in []time.Time) []time.Time {
	if len(in) == 0 {
		return nil
	}
	out := make([]time.Time, len(in))
	copy(out, in)
	return out
}

func (r *Ranker) rawScore(id string, now time.Time) float64 {
	timestamps := r.selections[id]
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
	scores := make([]float64, len(r.orderedIDs))
	maxScore := 0.0
	for docIndex, id := range r.orderedIDs {
		score := r.rawScore(id, now)
		if score <= 0 {
			continue
		}
		scores[docIndex] = score
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore > 0 {
		for i := range scores {
			scores[i] /= maxScore
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

// Score returns the normalized popularity score for a document slice index.
// It satisfies xsearch.Scorer.
func (r *Ranker) Score(docIndex int) float64 {
	return r.ScoreView().Popularity(docIndex)
}

// PopularityScore returns the raw exponential decay score.
func (r *Ranker) PopularityScore(id string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rawScore(id, time.Now())
}

// CombinedScore blends relevance with popularity using the ranker's alpha.
func (r *Ranker) CombinedScore(docIndex int, relevance float64) float64 {
	return r.ScoreView().CombinedScore(docIndex, relevance)
}

// ScoreView returns an immutable scorer view.
func (r *Ranker) ScoreView() ScoreView {
	snapshot, alpha := r.snapshotView()
	return ScoreView{
		scores: snapshot.scores,
		alpha:  alpha,
	}
}

// Popularity returns the normalized popularity score for docIndex.
func (s ScoreView) Popularity(docIndex int) float64 {
	if s.scores == nil || docIndex < 0 || docIndex >= len(s.scores) {
		return 0
	}
	return s.scores[docIndex]
}

// Score returns exactly the Popularity score to implement xsearch.Scorer.
func (s ScoreView) Score(docIndex int) float64 {
	return s.Popularity(docIndex)
}

// CombinedScore blends relevance with popularity using the ranker's configured alpha.
func (s ScoreView) CombinedScore(docIndex int, relevance float64) float64 {
	pop := s.Popularity(docIndex)
	return s.alpha*relevance + (1-s.alpha)*pop
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

// Load reads the current string-keyed format from path.
func (r *Ranker) Load(path string) error {
	_, err := r.LoadWithMigration(path, nil)
	return err
}

// LoadWithMigration reads popularity data and migrates the legacy integer-keyed
// format using orderedIDs when necessary. It returns true when migration occurred.
func (r *Ranker) LoadWithMigration(path string, orderedIDs []string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("ranking: reading %s: %w", path, err)
	}

	var raw map[string][]time.Time
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, fmt.Errorf("ranking: unmarshaling selections: %w", err)
	}

	migrated := false
	mapped := make(map[string][]time.Time, len(raw))
	allNumeric := len(raw) > 0
	for key := range raw {
		if _, err := strconv.Atoi(key); err != nil {
			allNumeric = false
			break
		}
	}

	if allNumeric {
		if len(orderedIDs) == 0 {
			return false, fmt.Errorf("ranking: legacy popularity data requires ordered IDs for migration")
		}
		for key, timestamps := range raw {
			idx, _ := strconv.Atoi(key)
			if idx < 0 || idx >= len(orderedIDs) {
				continue
			}
			id := orderedIDs[idx]
			mapped[id] = append(mapped[id], timestamps...)
		}
		migrated = true
	} else {
		for key, timestamps := range raw {
			mapped[key] = slicesCloneTimes(timestamps)
		}
	}

	r.mu.Lock()
	r.selections = mapped
	r.snapshotDirty = true
	r.mu.Unlock()
	return migrated, nil
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
