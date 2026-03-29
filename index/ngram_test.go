package index

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractTrigrams(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"shoes", []string{"sho", "hoe", "oes"}},
		{"hi", nil},
		{"a", nil},
		{"", nil},
		{"abc", []string{"abc"}},
		{"SHOES", []string{"sho", "hoe", "oes"}},
		{"  Nike  ", []string{"nik", "ike"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractTrigrams(tt.input)
			sort.Strings(got)
			expected := tt.expected
			sort.Strings(expected)
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("ExtractTrigrams(%q) = %v, want %v", tt.input, got, expected)
			}
		})
	}
}
