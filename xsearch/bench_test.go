package xsearch

import "testing"

func BenchmarkBloomMayContain(b *testing.B) {
	bloom := NewBloom(1000, 10)
	bloom.Add("bud")
	bloom.Add("udw")
	bloom.Add("dwe")
	b.ResetTimer()
	for b.Loop() {
		bloom.MayContain("bud")
	}
}

func BenchmarkExtractTrigramsMedium(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		extractNormalizedTrigrams(nil, "budweiser")
	}
}

func BenchmarkBM25Search(b *testing.B) {
	engine, err := New(testItems())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		engine.bm25.Search("budweiser")
	}
}

func BenchmarkNgramSearch(b *testing.B) {
	engine, err := New(testItems())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		engine.ngram.Search("budwiser")
	}
}
