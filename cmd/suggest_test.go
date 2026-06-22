package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLevenshtein(t *testing.T) {
	assert.Equal(t, 0, levenshtein("name", "name"))
	assert.Equal(t, 1, levenshtein("nam", "name"))
	assert.Equal(t, 2, levenshtein("naem", "name")) // transposition = 2 edits
	assert.Equal(t, 4, levenshtein("", "name"))
}

func TestClosestFieldName(t *testing.T) {
	cands := []string{"Development/Name", "Development/Priority", "Development/State"}

	// Close typo → suggestion (compared on the local name after "/").
	got, ok := closestFieldName("Development/Naem", cands)
	assert.True(t, ok)
	assert.Equal(t, "Development/Name", got)

	// Wrong space prefix but right local name still matches on local name.
	got, ok = closestFieldName("Wrong/Name", cands)
	assert.True(t, ok)
	assert.Equal(t, "Development/Name", got)

	// Nothing close → no suggestion.
	_, ok = closestFieldName("Development/Zzzzzzzz", cands)
	assert.False(t, ok)

	// No candidates → no suggestion.
	_, ok = closestFieldName("Development/Name", nil)
	assert.False(t, ok)
}
