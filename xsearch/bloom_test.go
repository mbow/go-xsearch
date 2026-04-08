package xsearch

import "testing"

func TestBloomAddAndMayContain(t *testing.T) {
	bloom := NewBloom(1000, 10)
	bloom.Add("sho")
	bloom.Add("hoe")
	bloom.Add("oes")
	if !bloom.MayContain("sho") || !bloom.MayContain("hoe") || !bloom.MayContain("oes") {
		t.Fatal("expected bloom hits")
	}
}

func TestBloomRareFalsePositives(t *testing.T) {
	bloom := NewBloom(1000, 20)
	bloom.Add("abc")
	bloom.Add("def")
	falsePositives := 0
	for _, s := range []string{"zzz", "yyy", "xxx", "www"} {
		if bloom.MayContain(s) {
			falsePositives++
		}
	}
	if falsePositives > 1 {
		t.Fatalf("too many false positives: %d", falsePositives)
	}
}

