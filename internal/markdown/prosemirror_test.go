package markdown

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seqIDs returns an id generator giving id-1, id-2, …
func seqIDs() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("id-%d", n) }
}

// toDocMap converts md and decodes the result.
func toDocMap(t *testing.T, md string) map[string]any {
	t.Helper()
	raw, err := ToDoc(md, seqIDs())
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

// stripIDs removes the generated attrs.id/guid from every node except mentions
// and entity references, whose id is the referenced entity.
func stripIDs(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if attrs, ok := x["attrs"].(map[string]any); ok && x["type"] != "mention" && x["type"] != "entity" {
			delete(attrs, "id")
			delete(attrs, "guid")
		}
		for k, c := range x {
			x[k] = stripIDs(c)
		}
	case []any:
		for i, c := range x {
			x[i] = stripIDs(c)
		}
	}
	return v
}

// The testdata pairs are Markdown and the ProseMirror doc Fibery's own import
// produced for it (node ids removed, entity ids replaced). The local converter
// must give the same document.
func TestToDoc_MatchesFiberyImport(t *testing.T) {
	mds, err := filepath.Glob("testdata/import*.md")
	require.NoError(t, err)
	require.NotEmpty(t, mds)
	for _, mdPath := range mds {
		t.Run(filepath.Base(mdPath), func(t *testing.T) {
			md, err := os.ReadFile(mdPath)
			require.NoError(t, err)
			wantRaw, err := os.ReadFile(mdPath[:len(mdPath)-len(".md")] + ".json")
			require.NoError(t, err)
			var want any
			require.NoError(t, json.Unmarshal(wantRaw, &want))

			got := stripIDs(toDocMap(t, string(md)))
			gotJSON, _ := json.MarshalIndent(got, "", "  ")
			wantJSON, _ := json.MarshalIndent(want, "", "  ")
			assert.JSONEq(t, string(wantJSON), string(gotJSON))
		})
	}
}

func TestToDoc_FreshIDs(t *testing.T) {
	doc := toDocMap(t, "# H\n\ntext\n\n- a\n\n```\nx\n```")
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if attrs, ok := x["attrs"].(map[string]any); ok {
				id, _ := attrs["id"].(string)
				require.NotEmpty(t, id, "every block node carries an id: %v", x["type"])
				assert.False(t, seen[id], "id %s reused", id)
				seen[id] = true
				if g, ok := attrs["guid"]; ok {
					assert.Equal(t, id, g, "guid equals id, as Fibery writes it")
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc["content"])
	assert.Len(t, seen, 6) // heading, paragraph, list, item, item paragraph, code
}

func topNodes(t *testing.T, md string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, n := range toDocMap(t, md)["content"].([]any) {
		out = append(out, stripIDs(n).(map[string]any))
	}
	return out
}

func TestToDoc_Empty(t *testing.T) {
	raw, err := ToDoc("  \n\n", seqIDs())
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"doc"}`, string(raw))
}

func TestToDoc_SingleTildeIsText(t *testing.T) {
	n := topNodes(t, "~5 min and ~10 min, ~~gone~~")
	require.Len(t, n, 1)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "~5 min and ~10 min, "},
		map[string]any{"type": "text", "text": "gone", "marks": []any{map[string]any{"type": "strike"}}},
	}, n[0]["content"])
}

func TestToDoc_NativeCalloutAnyColor(t *testing.T) {
	n := topNodes(t, "> [//]: # (callout;icon-type=emoji;icon=rocket;color=#00ff00)\n> go")
	require.Len(t, n, 1)
	assert.Equal(t, "callout", n[0]["type"])
	assert.Equal(t, map[string]any{"iconType": "emoji", "icon": "rocket", "color": "#00ff00"}, n[0]["attrs"])
	assert.Len(t, n[0]["content"], 1)
}

func TestToDoc_CalloutWithoutBody(t *testing.T) {
	n := topNodes(t, "> [!NOTE]")
	require.Len(t, n, 1)
	assert.Equal(t, "callout", n[0]["type"])
	assert.Equal(t, []any{map[string]any{"type": "paragraph", "attrs": map[string]any{}}}, n[0]["content"])
}

func TestToDoc_CalloutLineInCodeStaysCode(t *testing.T) {
	line := "> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)"
	n := topNodes(t, "```\n"+line+"\n```\n\n> [!NOTE]\n> hi")
	require.Len(t, n, 2)
	assert.Equal(t, "code_block", n[0]["type"])
	assert.Equal(t, line, n[0]["content"].([]any)[0].(map[string]any)["text"])
	assert.Equal(t, "callout", n[1]["type"])
}

func TestToDoc_CalloutLineMidQuoteVanishes(t *testing.T) {
	n := topNodes(t, "> first\n>\n> [//]: # (callout;icon=x;color=#d40915)\n> after")
	require.Len(t, n, 1)
	assert.Equal(t, "blockquote", n[0]["type"])
	b, _ := json.Marshal(n[0])
	assert.NotContains(t, string(b), "fiberycallout")
	assert.Contains(t, string(b), "after")
}

func TestToDoc_TokensNotInCode(t *testing.T) {
	ref := "[[#@11111111-1111-4111-8111-111111111111/22222222-2222-4222-8222-222222222222]]"
	n := topNodes(t, "`"+ref+" :fire:` :fire: :nope:")
	require.Len(t, n, 1)
	c := n[0]["content"].([]any)
	require.Len(t, c, 4)
	assert.Equal(t, ref+" :fire:", c[0].(map[string]any)["text"])
	assert.Equal(t, "emoji", c[2].(map[string]any)["type"])
	assert.Equal(t, " :nope:", c[3].(map[string]any)["text"])
}

func TestToDoc_EscapesAndEntities(t *testing.T) {
	n := topNodes(t, `\&amp; &amp; &bogus; \_x\_ &#65;`)
	require.Len(t, n, 1)
	assert.Equal(t, "&amp; & &bogus; _x_ A", n[0]["content"].([]any)[0].(map[string]any)["text"])
}
