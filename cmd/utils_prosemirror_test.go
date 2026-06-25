package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// sampleDoc: heading "AB" then paragraph "Hello world".
// Positions: heading opens@0, A@1, B@2, close@3; para opens@4, H@5,e@6,l@7,l@8,o@9,' '@10,w@11,o@12,r@13,l@14,d@15.
func sampleDoc() map[string]any {
	return map[string]any{
		"type": "doc",
		"content": []any{
			map[string]any{"type": "heading", "attrs": map[string]any{"level": float64(1)},
				"content": []any{map[string]any{"type": "text", "text": "AB"}}},
			map[string]any{"type": "paragraph",
				"content": []any{map[string]any{"type": "text", "text": "Hello world"}}},
		},
	}
}

func TestPMFindRange(t *testing.T) {
	d := sampleDoc()
	from, to, err := pmFindRange(d, "world", 0)
	assert.NoError(t, err)
	assert.Equal(t, 11, from)
	assert.Equal(t, 16, to)

	from, to, err = pmFindRange(d, "Hello", 0)
	assert.NoError(t, err)
	assert.Equal(t, 5, from)
	assert.Equal(t, 10, to)

	from, to, err = pmFindRange(d, "AB", 0)
	assert.NoError(t, err)
	assert.Equal(t, 1, from)
	assert.Equal(t, 3, to)
}

func TestPMFindRangeAmbiguous(t *testing.T) {
	d := sampleDoc()
	_, _, err := pmFindRange(d, "l", 0) // 3 matches
	assert.Error(t, err)
	// occurrence picks one (2nd 'l' is at pos 8)
	from, to, err := pmFindRange(d, "l", 2)
	assert.NoError(t, err)
	assert.Equal(t, 8, from)
	assert.Equal(t, 9, to)
}

func TestPMFindRangeNotFound(t *testing.T) {
	_, _, err := pmFindRange(sampleDoc(), "zzz", 0)
	assert.Error(t, err)
}

func TestPMSliceText(t *testing.T) {
	assert.Equal(t, "world", pmSliceText(sampleDoc(), 11, 16))
	assert.Equal(t, "Hello", pmSliceText(sampleDoc(), 5, 10))
}

func TestPMDocText(t *testing.T) {
	assert.Equal(t, "ABHello world", pmDocText(sampleDoc()))
}
