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
	r.RecordSelection("a")
	r.RecordSelection("a")
	r.RecordSelection("b")

	scoreA := r.PopularityScore("a")
	scoreB := r.PopularityScore("b")
	if scoreA <= scoreB {
		t.Fatalf("expected a > b: %f <= %f", scoreA, scoreB)
	}
}

func TestPopularityScoreDecay(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections("recent", []time.Time{now})
	r.SetSelections("old", []time.Time{now.Add(-14 * 24 * time.Hour)})

	scoreRecent := r.PopularityScore("recent")
	scoreOld := r.PopularityScore("old")
	if scoreRecent <= scoreOld {
		t.Fatalf("expected recent > old: %f <= %f", scoreRecent, scoreOld)
	}

	expectedDecay := math.Exp(-0.05 * 14)
	if math.Abs(scoreOld-expectedDecay) > 0.01 {
		t.Fatalf("expected ~%f, got %f", expectedDecay, scoreOld)
	}
}

func TestScoreViewPopularity(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	r.SetIDs([]string{"a", "b"})
	now := time.Now()
	r.SetSelections("a", []time.Time{now, now, now})
	r.SetSelections("b", []time.Time{now})

	view := r.ScoreView()
	if got := view.Popularity(0); got != 1.0 {
		t.Fatalf("expected 1.0, got %f", got)
	}
	if got := view.Popularity(1); got <= 0 || got >= 1 {
		t.Fatalf("expected value in (0,1), got %f", got)
	}
}

func TestCombinedScore(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	r.SetIDs([]string{"popular", "less-popular"})
	now := time.Now()
	r.SetSelections("popular", []time.Time{now, now, now})
	r.SetSelections("less-popular", []time.Time{now})

	score1 := r.CombinedScore(0, 0.3)
	score2 := r.CombinedScore(1, 0.9)
	if score2 <= score1 {
		t.Fatalf("expected higher relevance to win: %f <= %f", score2, score1)
	}
}

func TestScoreViewMatchesCombinedScore(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	r.SetIDs([]string{"a", "b"})
	now := time.Now()
	r.SetSelections("a", []time.Time{now, now})
	r.SetSelections("b", []time.Time{now})

	view := r.ScoreView()
	if got, want := view.Popularity(0), r.ScoreView().Popularity(0); math.Abs(got-want) > 1e-9 {
		t.Fatalf("popularity mismatch: %f != %f", got, want)
	}
	if got, want := view.CombinedScore(1, 0.75), r.CombinedScore(1, 0.75); math.Abs(got-want) > 1e-9 {
		t.Fatalf("score mismatch: %f != %f", got, want)
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections("a", []time.Time{now, now.Add(-24 * time.Hour)})
	r.SetSelections("b", []time.Time{now.Add(-48 * time.Hour)})

	dir := t.TempDir()
	path := filepath.Join(dir, "popularity.json")
	if err := r.Save(path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	r2 := New(0.05, 0.6)
	if err := r2.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	r2.mu.RLock()
	defer r2.mu.RUnlock()
	if len(r2.selections["a"]) != 2 {
		t.Fatalf("expected 2 selections for a, got %d", len(r2.selections["a"]))
	}
}

func TestLoadWithMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	now := time.Now().UTC().Truncate(time.Second)
	data := []byte(`{"0":["` + now.Format(time.RFC3339) + `"],"2":["` + now.Format(time.RFC3339) + `"]}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	r := New(0.05, 0.6)
	migrated, err := r.LoadWithMigration(path, []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("LoadWithMigration() error: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration")
	}
	if len(r.selections["first"]) != 1 || len(r.selections["third"]) != 1 {
		t.Fatalf("unexpected migrated selections: %+v", r.selections)
	}
}

func TestPrune(t *testing.T) {
	t.Parallel()
	r := New(0.05, 0.6)
	now := time.Now()
	r.SetSelections("a", []time.Time{now, now.Add(-91 * 24 * time.Hour)})
	r.Prune(90)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.selections["a"]) != 1 {
		t.Fatalf("expected 1 selection after prune, got %d", len(r.selections["a"]))
	}
}
