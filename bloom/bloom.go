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

// hashes returns k bit positions for the given item using double hashing.
func (f *Filter) hashes(item string) []uint64 {
	h1 := fnv1a(item)
	h2 := djb2(item)
	positions := make([]uint64, f.k)
	for i := 0; i < f.k; i++ {
		positions[i] = (h1 + uint64(i)*h2) % f.size
	}
	return positions
}

// Add inserts an item into the Bloom filter.
func (f *Filter) Add(item string) {
	for _, pos := range f.hashes(item) {
		word := pos / 64
		bit := pos % 64
		f.bits[word] |= 1 << bit
	}
}

// MayContain checks if an item might be in the filter.
// Returns false if the item is definitely not in the set.
// Returns true if the item might be in the set (possible false positive).
func (f *Filter) MayContain(item string) bool {
	for _, pos := range f.hashes(item) {
		word := pos / 64
		bit := pos % 64
		if f.bits[word]&(1<<bit) == 0 {
			return false
		}
	}
	return true
}
