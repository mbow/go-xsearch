package ranking

import (
	"math"
	"testing"
	"time"
)

func TestRecordSelection(t *testing.T) {
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
	r := New(0.05, 0.6)
	score := r.PopularityScore(99)
	if score != 0 {
		t.Errorf("expected 0 for unselected product, got %f", score)
	}
}

func TestNormalizedPopularity(t *testing.T) {
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
	r := New(0.05, 0.6)
	norm := r.NormalizedPopularity(1)
	if norm != 0 {
		t.Errorf("expected 0 when no selections exist, got %f", norm)
	}
}
