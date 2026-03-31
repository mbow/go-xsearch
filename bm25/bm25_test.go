package bm25

import (
	"slices"
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		input string
		want []string
	}{
		{"simple", "Budweiser", []string{"budweiser"}},
		{"multi word", "Nike Air Max", []string{"nike", "air", "max"}},
		{"punctuation", "Ben & Jerry's", []string{"ben", "jerry's"}},
		{"extra spaces", "  hello   world  ", []string{"hello", "world"}},
		{"empty", "", nil},
		{"number", "No. 12 Light", []string{"no.", "12", "light"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
