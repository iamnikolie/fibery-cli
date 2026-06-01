package render_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/langgerone/fibery-cli/internal/render"
)

func TestJSON_Pretty(t *testing.T) {
	var buf bytes.Buffer
	raw := json.RawMessage(`{"name":"Alice","age":30}`)
	require.NoError(t, render.JSON(&buf, raw))
	out := buf.String()
	assert.Contains(t, out, "Alice")
	assert.Contains(t, out, "30")
}

func TestList_Empty(t *testing.T) {
	var buf bytes.Buffer
	raw := json.RawMessage(`[]`)
	require.NoError(t, render.List(&buf, raw))
	assert.Contains(t, buf.String(), "No results")
}

func TestList_Items(t *testing.T) {
	var buf bytes.Buffer
	raw := json.RawMessage(`[{"Name":"Task A","State":"Open"},{"Name":"Task B","State":"Done"}]`)
	require.NoError(t, render.List(&buf, raw))
	out := buf.String()
	assert.Contains(t, out, "Task A")
	assert.Contains(t, out, "Done")
	// Markdown table has header separator
	assert.True(t, strings.Contains(out, "---"))
}
