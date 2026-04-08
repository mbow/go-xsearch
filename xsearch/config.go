package xsearch

import "fmt"

type optionKind uint8

const (
	optionScorer optionKind = iota + 1
	optionBloom
	optionBM25
	optionAlpha
	optionLimit
	optionFallbackField
)

type engineConfig struct {
	bloomBitsPerItem int
	bm25K1           float64
	bm25B            float64
	alpha            float64
	limit            int
	fallbackField    string
}

func defaultConfig() engineConfig {
	return engineConfig{
		bm25K1: 1.2,
		bm25B:  0.75,
		alpha:  0.6,
		limit:  10,
	}
}

// Option configures the engine.
type Option struct {
	kind  optionKind
	apply func(*engineConfig)
}

func (o Option) applyTo(cfg *engineConfig) {
	if o.apply != nil {
		o.apply(cfg)
	}
}

func (o Option) validateForSnapshotLoad() error {
	switch o.kind {
	case optionAlpha, optionLimit:
		return nil
	case optionBloom:
		return fmt.Errorf("xsearch: WithBloom cannot be used with NewFromSnapshot")
	case optionBM25:
		return fmt.Errorf("xsearch: WithBM25 cannot be used with NewFromSnapshot")
	case optionFallbackField:
		return fmt.Errorf("xsearch: WithFallbackField cannot be used with NewFromSnapshot")
	default:
		return nil
	}
}

func (c engineConfig) validate() error {
	if c.alpha < 0 || c.alpha > 1 {
		return fmt.Errorf("xsearch: alpha must be in [0, 1], got %f", c.alpha)
	}
	if c.limit < 2 || c.limit > 100 {
		return fmt.Errorf("xsearch: limit must be in [2, 100], got %d", c.limit)
	}
	if c.bloomBitsPerItem < 0 {
		return fmt.Errorf("xsearch: bloom bits per item must be >= 0, got %d", c.bloomBitsPerItem)
	}
	if c.bm25K1 <= 0 {
		return fmt.Errorf("xsearch: BM25 k1 must be > 0, got %f", c.bm25K1)
	}
	if c.bm25B < 0 || c.bm25B > 1 {
		return fmt.Errorf("xsearch: BM25 b must be in [0, 1], got %f", c.bm25B)
	}
	return nil
}


// WithBloom enables bloom filter pre-rejection.
func WithBloom(bitsPerItem int) Option {
	return Option{
		kind: optionBloom,
		apply: func(c *engineConfig) {
			c.bloomBitsPerItem = bitsPerItem
		},
	}
}

// WithBM25 configures BM25 parameters.
func WithBM25(k1, b float64) Option {
	return Option{
		kind: optionBM25,
		apply: func(c *engineConfig) {
			c.bm25K1 = k1
			c.bm25B = b
		},
	}
}

// WithAlpha sets the blend between relevance and external score.
func WithAlpha(alpha float64) Option {
	return Option{
		kind: optionAlpha,
		apply: func(c *engineConfig) {
			c.alpha = alpha
		},
	}
}

// WithLimit sets the max result count.
func WithLimit(n int) Option {
	return Option{
		kind: optionLimit,
		apply: func(c *engineConfig) {
			c.limit = n
		},
	}
}

// WithFallbackField sets the field used for fallback grouping.
func WithFallbackField(fieldName string) Option {
	return Option{
		kind: optionFallbackField,
		apply: func(c *engineConfig) {
			c.fallbackField = fieldName
		},
	}
}

