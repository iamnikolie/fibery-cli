package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppendInlineImages(t *testing.T) {
	img1 := "![a.png](/api/files/s1)"
	img2 := "![b.png](/api/files/s2)"

	t.Run("no images returns content unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", appendInlineImages("hello", nil))
		assert.Equal(t, "", appendInlineImages("", nil))
	})

	t.Run("text plus one image separated by a blank line", func(t *testing.T) {
		assert.Equal(t, "see repro\n\n"+img1, appendInlineImages("see repro", []string{img1}))
	})

	t.Run("text plus multiple images each its own paragraph", func(t *testing.T) {
		assert.Equal(t, "before/after\n\n"+img1+"\n\n"+img2, appendInlineImages("before/after", []string{img1, img2}))
	})

	t.Run("empty content yields image with no leading separator", func(t *testing.T) {
		assert.Equal(t, img1, appendInlineImages("", []string{img1}))
		assert.Equal(t, img1+"\n\n"+img2, appendInlineImages("", []string{img1, img2}))
	})
}
