//go:generate go run ../cmd/generate/main.go -input ../data/products.json -output data.cbor

package catalog

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

//go:embed data.cbor
var rawCBOR []byte

// payload is the full serialized dataset from the generator.
// BloomRaw and IndexRaw are kept as [cbor.RawMessage] to avoid importing
// bloom/index packages (which would create an import cycle).
type payload struct {
	Products []Product       `cbor:"products"`
	BloomRaw cbor.RawMessage `cbor:"bloom"`
	IndexRaw cbor.RawMessage `cbor:"index"`
	BM25Raw  cbor.RawMessage `cbor:"bm25"`
}

var (
	decoded        payload
	productsByName map[string]*Product
	productsByID   map[int]*Product
	initOnce       sync.Once
	initErr        error
)

func initEmbedded() {
	initOnce.Do(func() {
		if len(rawCBOR) == 0 {
			initErr = fmt.Errorf("catalog: embedded CBOR data is empty (run go generate ./catalog/)")
			return
		}

		gzReader, err := gzip.NewReader(bytes.NewReader(rawCBOR))
		if err != nil {
			initErr = fmt.Errorf("catalog: decompressing gzip: %w", err)
			return
		}
		defer gzReader.Close()

		cborData, err := io.ReadAll(gzReader)
		if err != nil {
			initErr = fmt.Errorf("catalog: reading gzip: %w", err)
			return
		}

		if err := cbor.Unmarshal(cborData, &decoded); err != nil {
			initErr = fmt.Errorf("catalog: unmarshaling CBOR: %w", err)
			return
		}

		productsByName = make(map[string]*Product, len(decoded.Products))
		productsByID = make(map[int]*Product, len(decoded.Products))
		for i := range decoded.Products {
			productsByName[decoded.Products[i].Name] = &decoded.Products[i]
			productsByID[i] = &decoded.Products[i]
		}
	})
}

// EmbeddedProducts returns all products from the compile-time embedded CBOR data.
// The first call decompresses and unmarshals; subsequent calls return cached data.
func EmbeddedProducts() ([]Product, error) {
	initEmbedded()
	return decoded.Products, initErr
}

// EmbeddedBloomRaw returns the raw CBOR bytes of the pre-built Bloom filter snapshot.
// The caller (typically [engine.NewFromEmbedded]) unmarshals this into [bloom.Snapshot].
func EmbeddedBloomRaw() ([]byte, error) {
	initEmbedded()
	return []byte(decoded.BloomRaw), initErr
}

// EmbeddedIndexRaw returns the raw CBOR bytes of the pre-built n-gram index snapshot.
// The caller (typically [engine.NewFromEmbedded]) unmarshals this into [index.Snapshot].
func EmbeddedIndexRaw() ([]byte, error) {
	initEmbedded()
	return []byte(decoded.IndexRaw), initErr
}

// EmbeddedBM25Raw returns the raw CBOR bytes of the pre-built BM25 index snapshot.
// The caller (typically [engine.NewFromEmbedded]) unmarshals this into [bm25.Snapshot].
func EmbeddedBM25Raw() ([]byte, error) {
	initEmbedded()
	return []byte(decoded.BM25Raw), initErr
}

// GetByName looks up a product by exact name. Returns nil, nil if not found.
func GetByName(name string) (*Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	return productsByName[name], nil
}

// GetByID looks up a product by its index position. Returns nil, nil if not found.
func GetByID(id int) (*Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	return productsByID[id], nil
}

// EmbeddedCount returns the number of embedded products.
func EmbeddedCount() (int, error) {
	initEmbedded()
	return len(decoded.Products), initErr
}
