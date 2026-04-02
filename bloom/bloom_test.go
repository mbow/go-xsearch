package bloom

import "testing"

func TestFnv1a(t *testing.T) {
	t.Parallel()
	h1 := fnv1a("hello")
	h2 := fnv1a("hello")
	h3 := fnv1a("world")

	if h1 != h2 {
		t.Errorf("same input should produce same hash: %d != %d", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different inputs should (usually) produce different hashes")
	}
	if h1 == 0 {
		t.Errorf("hash should not be zero for non-empty input")
	}
}

func TestDjb2(t *testing.T) {
	t.Parallel()
	h1 := djb2("hello")
	h2 := djb2("hello")
	h3 := djb2("world")

	if h1 != h2 {
		t.Errorf("same input should produce same hash: %d != %d", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different inputs should (usually) produce different hashes")
	}
	if h1 == 0 {
		t.Errorf("hash should not be zero for non-empty input")
	}
}

func TestHashIndependence(t *testing.T) {
	t.Parallel()
	f := fnv1a("test")
	d := djb2("test")
	if f == d {
		t.Errorf("fnv1a and djb2 should produce different hashes for same input: both %d", f)
	}
}

func TestNewFilter(t *testing.T) {
	t.Parallel()
	f := New(1000, 3)
	if f == nil {
		t.Fatal("New() returned nil")
	}
}

func TestAddAndMayContain(t *testing.T) {
	t.Parallel()
	f := New(1000, 3)
	f.Add("sho")
	f.Add("hoe")
	f.Add("oes")

	if !f.MayContain("sho") {
		t.Error("MayContain('sho') should return true after Add")
	}
	if !f.MayContain("hoe") {
		t.Error("MayContain('hoe') should return true after Add")
	}
	if !f.MayContain("oes") {
		t.Error("MayContain('oes') should return true after Add")
	}
}

func TestMayContainNeverAdded(t *testing.T) {
	t.Parallel()
	f := New(20000, 3)

	f.Add("abc")
	f.Add("def")
	f.Add("ghi")

	// These were never added — with a large bit array, false positives are rare.
	notAdded := []string{"zzz", "qqq", "xxx", "yyy", "www", "vvv", "uuu", "ppp"}
	falsePositives := 0
	for _, s := range notAdded {
		if f.MayContain(s) {
			falsePositives++
		}
	}
	if falsePositives > 2 {
		t.Errorf("too many false positives: %d out of %d", falsePositives, len(notAdded))
	}
}

func TestNoFalseNegatives(t *testing.T) {
	t.Parallel()
	f := New(20000, 3)
	items := []string{"abc", "def", "ghi", "jkl", "mno", "pqr", "stu", "vwx"}

	for _, item := range items {
		f.Add(item)
	}

	for _, item := range items {
		if !f.MayContain(item) {
			t.Errorf("false negative for %q — this must never happen", item)
		}
	}
}
