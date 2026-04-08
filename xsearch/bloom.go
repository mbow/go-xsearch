package xsearch

// fnv1a computes the FNV-1a 64-bit hash for s.
func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// djb2 computes the DJB2 64-bit hash for s.
func djb2(s string) uint64 {
	h := uint64(5381)
	for i := range len(s) {
		h = ((h << 5) + h) + uint64(s[i])
	}
	return h
}

// Bloom is a Bloom filter backed by a []uint64 bit array.
type Bloom struct {
	bits []uint64
	size uint64
	k    int
}

type bloomSnapshot struct {
	Bits []uint64 `cbor:"bits"`
	Size uint64   `cbor:"size"`
	K    int      `cbor:"k"`
}

func (b *Bloom) snapshot() bloomSnapshot {
	return bloomSnapshot{Bits: b.bits, Size: b.size, K: b.k}
}

func bloomFromSnapshot(s bloomSnapshot) *Bloom {
	return &Bloom{bits: s.Bits, size: s.Size, k: s.K}
}

// NewBloom creates a bloom filter sized for n items.
func NewBloom(n int, bitsPerItem int) *Bloom {
	if n < 1 {
		n = 1
	}
	if bitsPerItem < 1 {
		bitsPerItem = 1
	}
	numBits := uint64(n * bitsPerItem)
	words := (numBits + 63) / 64
	return &Bloom{
		bits: make([]uint64, words),
		size: words * 64,
		k:    3,
	}
}

// Add inserts item into the filter.
func (b *Bloom) Add(item string) {
	h1 := fnv1a(item)
	h2 := djb2(item)
	for i := range b.k {
		pos := (h1 + uint64(i)*h2) % b.size
		b.bits[pos/64] |= 1 << (pos % 64)
	}
}

// MayContain reports whether item might be in the set.
func (b *Bloom) MayContain(item string) bool {
	h1 := fnv1a(item)
	h2 := djb2(item)
	for i := range b.k {
		pos := (h1 + uint64(i)*h2) % b.size
		if b.bits[pos/64]&(1<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}
