//go:generate go run ../cmd/generate/main.go -input ../data/products.json -output data.cbor

package catalog

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

//go:embed data.cbor
var rawCBOR []byte

// payload is the full serialized dataset from the generator.
// BloomRaw and IndexRaw are kept as raw CBOR bytes to avoid importing
// bloom/index packages (which would create an import cycle).
type payload struct {
	Products []Product       `cbor:"products"`
	BloomRaw cbor.RawMessage `cbor:"bloom"`
	IndexRaw cbor.RawMessage `cbor:"index"`
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

		if err := cbor.Unmarshal(rawCBOR, &decoded); err != nil {
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

// EmbeddedProducts returns all products from the compiled-in CBOR data.
func EmbeddedProducts() ([]Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	return decoded.Products, nil
}

// EmbeddedBloomRaw returns the raw CBOR bytes of the pre-built Bloom filter snapshot.
// The caller (engine) unmarshals this into bloom.Snapshot.
func EmbeddedBloomRaw() ([]byte, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	return []byte(decoded.BloomRaw), nil
}

// EmbeddedIndexRaw returns the raw CBOR bytes of the pre-built n-gram index snapshot.
// The caller (engine) unmarshals this into index.Snapshot.
func EmbeddedIndexRaw() ([]byte, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	return []byte(decoded.IndexRaw), nil
}

// GetByName looks up a product by exact name. Returns nil if not found.
func GetByName(name string) (*Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	p := productsByName[name]
	return p, nil
}

// GetByID looks up a product by its index position. Returns nil if not found.
func GetByID(id int) (*Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	p := productsByID[id]
	return p, nil
}

// EmbeddedCount returns the number of embedded products.
func EmbeddedCount() (int, error) {
	initEmbedded()
	if initErr != nil {
		return 0, initErr
	}
	return len(decoded.Products), nil
}
