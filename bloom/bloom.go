package bloom

// fnv1a computes FNV-1a hash for the given string.
func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// djb2 computes DJB2 hash for the given string.
func djb2(s string) uint64 {
	h := uint64(5381)
	for i := 0; i < len(s); i++ {
		h = ((h << 5) + h) + uint64(s[i])
	}
	return h
}

// Filter is a Bloom filter backed by a bit array.
type Filter struct {
	bits []uint64
	size uint64
	k    int
}

// Snapshot holds the serializable state of a Bloom filter.
type Snapshot struct {
	Bits []uint64 `cbor:"bits"`
	Size uint64   `cbor:"size"`
	K    int      `cbor:"k"`
}

// ToSnapshot exports the filter state for serialization.
func (f *Filter) ToSnapshot() Snapshot {
	return Snapshot{Bits: f.bits, Size: f.size, K: f.k}
}

// FromSnapshot restores a Bloom filter from serialized state.
func FromSnapshot(s Snapshot) *Filter {
	return &Filter{bits: s.Bits, size: s.Size, k: s.K}
}

// New creates a Bloom filter with the given number of bits and hash count.
func New(numBits uint64, k int) *Filter {
	// Round up to multiple of 64
	words := (numBits + 63) / 64
	return &Filter{
		bits: make([]uint64, words),
		size: words * 64,
		k:    k,
	}
}

// Add inserts an item into the Bloom filter.
func (f *Filter) Add(item string) {
	h1 := fnv1a(item)
	h2 := djb2(item)
	for i := 0; i < f.k; i++ {
		pos := (h1 + uint64(i)*h2) % f.size
		f.bits[pos/64] |= 1 << (pos % 64)
	}
}

// MayContain checks if an item might be in the filter.
// Returns false if the item is definitely not in the set.
// Returns true if the item might be in the set (possible false positive).
func (f *Filter) MayContain(item string) bool {
	h1 := fnv1a(item)
	h2 := djb2(item)
	for i := 0; i < f.k; i++ {
		pos := (h1 + uint64(i)*h2) % f.size
		if f.bits[pos/64]&(1<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}
