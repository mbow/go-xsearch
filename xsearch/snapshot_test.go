package xsearch

import "testing"

func TestSnapshotRoundTrip(t *testing.T) {
	engine, err := New(testItems(), WithBloom(100), WithFallbackField("category"), WithLimit(5))
	if err != nil {
		t.Fatal(err)
	}
	before := engine.Search("bud")
	snapshot, err := engine.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewFromSnapshot(snapshot, testItems(), WithLimit(5))
	if err != nil {
		t.Fatal(err)
	}
	after := restored.Search("bud")
	if len(before) != len(after) {
		t.Fatalf("result count mismatch: %d != %d", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].MatchType != after[i].MatchType {
			t.Fatalf("result[%d] mismatch: before=%+v after=%+v", i, before[i], after[i])
		}
	}
}

func TestSnapshotVersionRejection(t *testing.T) {
	engine, err := New(testItems())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot[4] = 99
	if _, err := NewFromSnapshot(snapshot, testItems()); err == nil {
		t.Fatal("expected version rejection")
	}
}

