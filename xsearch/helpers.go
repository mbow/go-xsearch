package xsearch

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultMinDirectResults = 3
	fallbackThreshold       = 0.2
	fallbackRelevance       = 0.1
)

type internalField struct {
	Name        string
	Values      []string
	LowerValues []string
	Weight      float64
}

type preparedItem struct {
	ID           string
	Fields       []internalField
	primaryField int
}

func cloneFields(fields []Field) []Field {
	if len(fields) == 0 {
		return nil
	}
	cloned := make([]Field, 0, len(fields))
	for _, f := range fields {
		values := make([]string, 0, len(f.Values))
		for _, v := range f.Values {
			if v == "" {
				continue
			}
			values = append(values, v)
		}
		cloned = append(cloned, Field{
			Name:   f.Name,
			Values: values,
			Weight: f.Weight,
		})
	}
	return cloned
}

func cloneItem(item preparedItem) Item {
	fields := make([]Field, len(item.Fields))
	for i, f := range item.Fields {
		fields[i] = Field{
			Name:   f.Name,
			Values: slices.Clone(f.Values),
			Weight: f.Weight,
		}
	}
	return Item{ID: item.ID, Fields: fields}
}

func normalizeQuery(s string) string {
	start := 0
	end := len(s)
	for start < end && isASCIIWhitespace(s[start]) {
		start++
	}
	for start < end && isASCIIWhitespace(s[end-1]) {
		end--
	}
	s = s[start:end]
	if s == "" {
		return ""
	}
	for i := range len(s) {
		c := s[i]
		if c >= utf8.RuneSelf || (c >= 'A' && c <= 'Z') {
			return strings.ToLower(s)
		}
	}
	return s
}

func isASCIIWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func extractNormalizedTrigrams(dst []string, s string) []string {
	if len(s) < 3 {
		return nil
	}
	need := len(s) - 2
	if cap(dst) < need {
		dst = make([]string, 0, need)
	} else {
		dst = dst[:0]
	}
	for i := range need {
		dst = append(dst, s[i:i+3])
	}
	return dst
}

func tokenize(s string) []string {
	var tokens []string
	for field := range strings.FieldsSeq(s) {
		lower := toLowerFast(field)
		if hasAlphanumeric(lower) {
			tokens = append(tokens, lower)
		}
	}
	return tokens
}

func toLowerFast(s string) string {
	for i := range len(s) {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return strings.ToLower(s)
		}
	}
	return s
}

func hasAlphanumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func appendQueryWords(dst []string, query string) []string {
	start := -1
	for i := 0; i <= len(query); i++ {
		if i < len(query) && !isASCIIWhitespace(query[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			dst = append(dst, query[start:i])
			start = -1
		}
	}
	return dst
}

func mergeHighlights(hs []Highlight) []Highlight {
	if len(hs) <= 1 {
		return hs
	}
	slices.SortFunc(hs, func(a, b Highlight) int {
		if a.ValueIndex != b.ValueIndex {
			return cmp.Compare(a.ValueIndex, b.ValueIndex)
		}
		if a.Start != b.Start {
			return cmp.Compare(a.Start, b.Start)
		}
		return cmp.Compare(a.End, b.End)
	})

	out := hs[:1]
	for _, cur := range hs[1:] {
		last := &out[len(out)-1]
		if cur.ValueIndex == last.ValueIndex && cur.Start <= last.End {
			last.End = max(last.End, cur.End)
			continue
		}
		out = append(out, cur)
	}
	return out
}

func sanitizeScorerValue(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}
