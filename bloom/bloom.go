// Package bloom implements a Bloom filter for probabilistic set membership testing.
//
// A Bloom filter can tell you "definitely not in the set" or "maybe in the set."
// It uses two independent hash functions (FNV-1a and DJB2) combined via double
// hashing to derive k bit positions per item. False positives are possible;
// false negatives are not.
package bloom

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

// Filter is a Bloom filter backed by a []uint64 bit array.
type Filter struct {
	bits []uint64
	size uint64
	k    int
}

// Snapshot holds the serializable state of a [Filter].
type Snapshot struct {
	Bits []uint64 `cbor:"bits"`
	Size uint64   `cbor:"size"`
	K    int      `cbor:"k"`
}

// ToSnapshot exports the filter state for serialization.
func (f *Filter) ToSnapshot() Snapshot {
	return Snapshot{Bits: f.bits, Size: f.size, K: f.k}
}

// FromSnapshot restores a [Filter] from a previously serialized [Snapshot].
func FromSnapshot(s Snapshot) *Filter {
	return &Filter{bits: s.Bits, size: s.Size, k: s.K}
}

// New creates a Bloom filter with numBits capacity and k hash functions.
// The bit array is rounded up to the nearest multiple of 64.
func New(numBits uint64, k int) *Filter {
	words := (numBits + 63) / 64
	return &Filter{
		bits: make([]uint64, words),
		size: words * 64,
		k:    k,
	}
}

// Add inserts item into the filter.
func (f *Filter) Add(item string) {
	h1 := fnv1a(item)
	h2 := djb2(item)
	for i := range f.k {
		pos := (h1 + uint64(i)*h2) % f.size
		f.bits[pos/64] |= 1 << (pos % 64)
	}
}

// MayContain reports whether item might be in the filter.
// It returns false if the item is definitely absent (no false negatives).
// A true return may be a false positive.
func (f *Filter) MayContain(item string) bool {
	h1 := fnv1a(item)
	h2 := djb2(item)
	for i := range f.k {
		pos := (h1 + uint64(i)*h2) % f.size
		if f.bits[pos/64]&(1<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}
