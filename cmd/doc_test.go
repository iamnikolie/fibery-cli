package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocBodyFromArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	require.NoError(t, os.WriteFile(path, []byte(`line\nstays literal`), 0o600))

	got, err := docBodyFromArgs([]string{"ref"}, path)
	require.NoError(t, err)
	assert.Equal(t, `line\nstays literal`, got, "--file is read verbatim")

	got, err = docBodyFromArgs([]string{"ref", `a\nb`}, "")
	require.NoError(t, err)
	assert.Equal(t, "a\nb", got, "positional text has its escapes interpreted")

	_, err = docBodyFromArgs([]string{"ref", "text"}, path)
	assert.ErrorContains(t, err, "not both")

	_, err = docBodyFromArgs([]string{"ref"}, "")
	assert.ErrorContains(t, err, "missing body")

	_, err = docBodyFromArgs([]string{"ref"}, filepath.Join(t.TempDir(), "absent.md"))
	assert.Error(t, err)
}

func TestDocModifiedAfter(t *testing.T) {
	since := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	assert.False(t, docModifiedAfter("2026-10-14T09:00:00.000Z", since), "same instant is unmodified")
	assert.False(t, docModifiedAfter("2026-10-14T08:59:59.999Z", since))
	assert.True(t, docModifiedAfter("2026-10-14T09:00:00.001Z", since))
	assert.True(t, docModifiedAfter("garbage", since), "unparseable date fails closed")
}

func TestParseDocJSON(t *testing.T) {
	envelope := `{"secret":"s","content":{"doc":{"type":"doc","content":[]},"comments":[{"id":"c1"}]},"modificationDate":"2026-10-14T09:00:00.000Z"}`
	c, hasComments, err := parseDocJSON([]byte(envelope))
	require.NoError(t, err)
	assert.True(t, hasComments)
	assert.JSONEq(t, `{"type":"doc","content":[]}`, string(c.Doc))
	assert.Len(t, c.Comments, 1)

	c, hasComments, err = parseDocJSON([]byte(`{"doc":{"type":"doc","content":[]}}`))
	require.NoError(t, err)
	assert.False(t, hasComments, "bare doc without comments keeps the document's own")
	assert.Equal(t, []any{}, c.Comments)

	_, hasComments, err = parseDocJSON([]byte(`{"doc":{"type":"doc","content":[]},"comments":null}`))
	require.NoError(t, err)
	assert.False(t, hasComments, "null comments keeps the document's own instead of wiping them")

	_, _, err = parseDocJSON([]byte(`{"type":"doc","content":[]}`))
	assert.ErrorContains(t, err, `no "doc" object`, "a bare ProseMirror node gets the helpful error")

	_, _, err = parseDocJSON([]byte(`{"comments":[]}`))
	assert.ErrorContains(t, err, `no "doc" object`)

	_, _, err = parseDocJSON([]byte(`not json`))
	assert.Error(t, err)
}
