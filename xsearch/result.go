package xsearch

// MatchType indicates how a result was found.
type MatchType int

// MatchType values.
const (
	MatchDirect   MatchType = iota // Found via direct n-gram or BM25 match
	MatchFallback                  // Found via fallback group
)

// Highlight marks a matched byte range within a field value.
type Highlight struct {
	Start      int // byte offset (inclusive)
	End        int // byte offset (exclusive)
	ValueIndex int // index into the matched Field.Values slice
}

// Result is a single search result with metadata.
type Result struct {
	ID         string
	Score      float64
	MatchType  MatchType
	Highlights map[string][]Highlight // field name -> highlight spans
}

