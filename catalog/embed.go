//go:generate go run ../cmd/generate/main.go -input ../data/products.json -output data.cbor

package catalog

import (
	"bytes"
	_ "embed"
	"fmt"
)

//go:embed data.cbor
var rawSnapshot []byte

// EmbeddedSnapshot returns the embedded xsearch snapshot bytes.
func EmbeddedSnapshot() ([]byte, error) {
	if len(rawSnapshot) == 0 {
		return nil, fmt.Errorf("catalog: embedded snapshot is empty (run go generate ./catalog/)")
	}
	return bytes.Clone(rawSnapshot), nil
}
