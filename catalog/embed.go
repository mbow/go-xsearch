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

var (
	embeddedProducts []Product
	productsByName   map[string]*Product
	productsByID     map[int]*Product
	initOnce         sync.Once
	initErr          error
)

func initEmbedded() {
	initOnce.Do(func() {
		if len(rawCBOR) == 0 {
			initErr = fmt.Errorf("catalog: embedded CBOR data is empty (run go generate ./catalog/)")
			return
		}

		if err := cbor.Unmarshal(rawCBOR, &embeddedProducts); err != nil {
			initErr = fmt.Errorf("catalog: unmarshaling CBOR: %w", err)
			return
		}

		productsByName = make(map[string]*Product, len(embeddedProducts))
		productsByID = make(map[int]*Product, len(embeddedProducts))
		for i := range embeddedProducts {
			productsByName[embeddedProducts[i].Name] = &embeddedProducts[i]
			productsByID[i] = &embeddedProducts[i]
		}
	})
}

// EmbeddedProducts returns all products from the compiled-in CBOR data.
// This is a zero-cost lookup after the first call — no file I/O, no parsing.
func EmbeddedProducts() ([]Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	return embeddedProducts, nil
}

// GetByName looks up a product by exact name. Returns nil if not found.
func GetByName(name string) (*Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	p, ok := productsByName[name]
	if !ok {
		return nil, nil
	}
	return p, nil
}

// GetByID looks up a product by its index position. Returns nil if not found.
func GetByID(id int) (*Product, error) {
	initEmbedded()
	if initErr != nil {
		return nil, initErr
	}
	p, ok := productsByID[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

// EmbeddedCount returns the number of embedded products.
func EmbeddedCount() (int, error) {
	initEmbedded()
	if initErr != nil {
		return 0, initErr
	}
	return len(embeddedProducts), nil
}
