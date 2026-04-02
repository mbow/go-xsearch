package ranking

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordSelection(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	r.RecordSelection(1)
	r.RecordSelection(1)
	r.RecordSelection(2)

	score1 := r.PopularityScore(1)
	score2 := r.PopularityScore(2)

	if score1 <= score2 {
		t.Errorf("product 1 (2 selections) should score higher than product 2 (1 selection): %f <= %f", score1, score2)
	}
}

func TestPopularityScoreDecay(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)

	now := time.Now()
	r.SetSelections(1, []time.Time{now})
	r.SetSelections(2, []time.Time{now.Add(-14 * 24 * time.Hour)}) // 14 days ago

	score1 := r.PopularityScore(1)
	score2 := r.PopularityScore(2)

	if score1 <= score2 {
		t.Errorf("recent selection should score higher than 14-day-old: %f <= %f", score1, score2)
	}

	// 14 days at lambda=0.05: e^(-0.05*14) ≈ 0.497
	expectedDecay := math.Exp(-0.05 * 14)
	if math.Abs(score2-expectedDecay) > 0.01 {
		t.Errorf("expected score2 ≈ %f, got %f", expectedDecay, score2)
	}
}

func TestPopularityScoreNoSelections(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	score := r.PopularityScore(99)
	if score != 0 {
		t.Errorf("expected 0 for unselected product, got %f", score)
	}
}

func TestNormalizedPopularity(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{now, now, now})
	r.SetSelections(2, []time.Time{now})

	norm1 := r.NormalizedPopularity(1)
	norm2 := r.NormalizedPopularity(2)

	if norm1 != 1.0 {
		t.Errorf("max popularity product should normalize to 1.0, got %f", norm1)
	}
	if norm2 <= 0 || norm2 >= 1.0 {
		t.Errorf("lower popularity should normalize between 0 and 1, got %f", norm2)
	}
}

func TestNormalizedPopularityNoSelections(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	norm := r.NormalizedPopularity(1)
	if norm != 0 {
		t.Errorf("expected 0 when no selections exist, got %f", norm)
	}
}

func TestCombinedScore(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{now, now, now})
	r.SetSelections(2, []time.Time{now})

	// Product 1: high popularity, low relevance
	score1 := r.CombinedScore(1, 0.3)
	// Product 2: low popularity, high relevance
	score2 := r.CombinedScore(2, 0.9)

	// With alpha=0.6, relevance dominates, so product 2 should win
	if score2 <= score1 {
		t.Errorf("high relevance should beat high popularity at alpha=0.6: score1=%f, score2=%f", score1, score2)
	}
}

func TestCombinedScoreZeroPopularity(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	score := r.CombinedScore(99, 0.8)
	// With no popularity data, score = alpha * relevance = 0.6 * 0.8 = 0.48
	expected := 0.6 * 0.8
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected %f, got %f", expected, score)
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{now, now.Add(-24 * time.Hour)})
	r.SetSelections(5, []time.Time{now.Add(-48 * time.Hour)})

	dir := t.TempDir()
	path := filepath.Join(dir, "popularity.json")

	if err := r.Save(path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	r2 := New(0.05, 0.6)
	if err := r2.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	r2.mu.RLock()
	defer r2.mu.RUnlock()

	if len(r2.selections[1]) != 2 {
		t.Errorf("expected 2 selections for product 1, got %d", len(r2.selections[1]))
	}
	if len(r2.selections[5]) != 1 {
		t.Errorf("expected 1 selection for product 5, got %d", len(r2.selections[5]))
	}
}

func TestLoadNonexistent(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	err := r.Load("/nonexistent/path.json")
	// Should not error — missing file means no prior data
	if err != nil {
		t.Errorf("Load() should not error for missing file, got: %v", err)
	}
}

func TestPrune(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections(1, []time.Time{
		now,
		now.Add(-91 * 24 * time.Hour), // older than 90 days
	})

	r.Prune(90)

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.selections[1]) != 1 {
		t.Errorf("expected 1 selection after prune, got %d", len(r.selections[1]))
	}
}
