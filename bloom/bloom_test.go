package bloom

import "testing"

func TestFnv1a(t *testing.T) {
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
	f := fnv1a("test")
	d := djb2("test")
	if f == d {
		t.Errorf("fnv1a and djb2 should produce different hashes for same input: both %d", f)
	}
}
