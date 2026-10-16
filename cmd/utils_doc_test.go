package cmd

import (
	"encoding/json"
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const appendAddition = `{"type":"doc","content":[` +
	`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Added"}]},` +
	`{"type":"paragraph","content":[{"type":"text","text":"more"}]}]}`

func TestAppendDocNodes_KeepsBodyAndComments(t *testing.T) {
	doc := `{"type":"doc","content":[` +
		`{"type":"paragraph","content":[{"type":"text","text":"ab"}]},` +
		`{"type":"fiberyView","attrs":{"viewId":"v1"}}]}`
	comments := []any{map[string]any{"from": float64(1), "to": float64(3), "id": "c1"}}
	content := client.DocContent{Doc: json.RawMessage(doc), Comments: comments}

	require.NoError(t, appendDocNodes(&content, json.RawMessage(appendAddition)))

	assert.Equal(t, []string{"paragraph", "fiberyView", "heading", "paragraph"}, topTypes(t, content.Doc))
	assert.Contains(t, string(content.Doc), `{"type":"fiberyView","attrs":{"viewId":"v1"}}`)
	assert.Equal(t, comments, content.Comments)
}

func TestAppendDocNodes_BlankDocument(t *testing.T) {
	blank := client.DocContent{Doc: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)}
	require.NoError(t, appendDocNodes(&blank, json.RawMessage(appendAddition)))
	assert.Equal(t, []string{"heading", "paragraph"}, topTypes(t, blank.Doc))

	null := client.DocContent{}
	require.NoError(t, appendDocNodes(&null, json.RawMessage(appendAddition)))
	assert.Equal(t, []string{"heading", "paragraph"}, topTypes(t, null.Doc))
}

func TestAppendDocNodes_EmptyAddition(t *testing.T) {
	content := client.DocContent{Doc: json.RawMessage(sizedDoc)}
	err := appendDocNodes(&content, json.RawMessage(`{"type":"doc","content":[]}`))
	assert.ErrorContains(t, err, "nothing to append")
	assert.JSONEq(t, sizedDoc, string(content.Doc))
}
