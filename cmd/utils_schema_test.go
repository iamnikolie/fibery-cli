package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClosestDatabase(t *testing.T) {
	dbs := []string{
		"Development/Dev Task",
		"Development/Dev epic",
		"Support platform/Support ticket",
		"Product/Feature",
	}

	// Typo in the space part → suggested on the full string.
	got, ok := closestDatabase("Develpment/Dev Task", dbs)
	assert.True(t, ok)
	assert.Equal(t, "Development/Dev Task", got)

	// Dropped space prefix → matched on the local (post-slash) name.
	got, ok = closestDatabase("Dev Task", dbs)
	assert.True(t, ok)
	assert.Equal(t, "Development/Dev Task", got)

	// Case-insensitive.
	got, ok = closestDatabase("development/dev task", dbs)
	assert.True(t, ok)
	assert.Equal(t, "Development/Dev Task", got)

	// Local-name match across a different space.
	got, ok = closestDatabase("Support ticket", dbs)
	assert.True(t, ok)
	assert.Equal(t, "Support platform/Support ticket", got)

	// Nothing close → no suggestion.
	_, ok = closestDatabase("Totally Unrelated Xyz", dbs)
	assert.False(t, ok)

	// No candidates → no suggestion.
	_, ok = closestDatabase("Development/Dev Task", nil)
	assert.False(t, ok)
}
