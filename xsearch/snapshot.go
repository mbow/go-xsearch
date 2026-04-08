package xsearch

import (
	"bytes"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

const (
	snapshotMagic   = "XSRC"
	snapshotVersion = byte(1)
)

type snapshotConfig struct {
	BloomBitsPerItem int     `cbor:"bloom_bits_per_item"`
	BM25K1           float64 `cbor:"bm25_k1"`
	BM25B            float64 `cbor:"bm25_b"`
	FallbackField    string  `cbor:"fallback_field,omitempty"`
	DefaultLimit     int     `cbor:"default_limit"`
}

type snapshotPayload struct {
	Config snapshotConfig `cbor:"config"`
	Bloom  *bloomSnapshot `cbor:"bloom,omitempty"`
	Ngram  ngramSnapshot  `cbor:"ngram"`
	BM25   bm25Snapshot   `cbor:"bm25"`
}

// Snapshot serializes the engine's indices to a self-contained CBOR blob.
func (e *Engine) Snapshot() ([]byte, error) {
	payload := snapshotPayload{
		Config: snapshotConfig{
			BloomBitsPerItem: e.cfg.bloomBitsPerItem,
			BM25K1:           e.cfg.bm25K1,
			BM25B:            e.cfg.bm25B,
			FallbackField:    e.cfg.fallbackField,
			DefaultLimit:     e.cfg.limit,
		},
		Ngram: e.ngram.snapshot(),
		BM25:  e.bm25.snapshot(),
	}
	if e.bloom != nil {
		snap := e.bloom.snapshot()
		payload.Bloom = &snap
	}

	em, err := cbor.EncOptions{Sort: cbor.SortCanonical}.EncMode()
	if err != nil {
		return nil, fmt.Errorf("xsearch: creating CBOR encoder: %w", err)
	}
	body, err := em.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("xsearch: marshaling snapshot: %w", err)
	}

	out := make([]byte, 0, len(snapshotMagic)+1+len(body))
	out = append(out, snapshotMagic...)
	out = append(out, snapshotVersion)
	out = append(out, body...)
	return out, nil
}

// NewFromSnapshot loads a pre-built engine from a self-contained CBOR snapshot.
func NewFromSnapshot[T Searchable](data []byte, items []T, opts ...Option) (*Engine, error) {
	if len(data) < len(snapshotMagic)+1 {
		return nil, fmt.Errorf("xsearch: snapshot too short")
	}
	if string(data[:len(snapshotMagic)]) != snapshotMagic {
		return nil, fmt.Errorf("xsearch: invalid snapshot magic")
	}
	if data[len(snapshotMagic)] != snapshotVersion {
		return nil, fmt.Errorf("xsearch: unsupported snapshot version %d", data[len(snapshotMagic)])
	}

	var payload snapshotPayload
	if err := cbor.NewDecoder(bytes.NewReader(data[len(snapshotMagic)+1:])).Decode(&payload); err != nil {
		return nil, fmt.Errorf("xsearch: decoding snapshot: %w", err)
	}

	cfg := defaultConfig()
	cfg.bloomBitsPerItem = payload.Config.BloomBitsPerItem
	cfg.bm25K1 = payload.Config.BM25K1
	cfg.bm25B = payload.Config.BM25B
	cfg.fallbackField = payload.Config.FallbackField
	if payload.Config.DefaultLimit != 0 {
		cfg.limit = payload.Config.DefaultLimit
	}
	for _, opt := range opts {
		if err := opt.validateForSnapshotLoad(); err != nil {
			return nil, err
		}
		opt.applyTo(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	prepared := make([]preparedItem, len(items))
	idToDoc := make(map[string]int, len(items))
	ordered := make([]string, len(items))
	for i, item := range items {
		id := item.SearchID()
		if id == "" {
			return nil, fmt.Errorf("xsearch: corrupted snapshot item at index %d has empty ID", i)
		}
		if _, ok := idToDoc[id]; ok {
			return nil, fmt.Errorf("xsearch: corrupted snapshot duplicate ID %q", id)
		}
		fields, primaryField, err := prepareFields(id, item.SearchFields())
		if err != nil {
			return nil, err
		}
		prepared[i] = preparedItem{
			ID:           id,
			Fields:       fields,
			primaryField: primaryField,
		}
		idToDoc[id] = i
		ordered[i] = id
	}

	e := &Engine{
		cfg:      cfg,
		items:    prepared,
		idToDoc:  idToDoc,
		ordered:  ordered,
		bm25:     bm25FromSnapshot(payload.BM25),
		ngram:    ngramFromSnapshot(payload.Ngram, len(prepared)),
		fallback: newFallbackIndex(prepared, cfg.fallbackField),
	}
	if payload.Bloom != nil {
		e.bloom = bloomFromSnapshot(*payload.Bloom)
	}
	return e, nil
}

