package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mvField(name, typ string, meta map[string]any) map[string]any {
	f := sdbField(name, typ)
	if meta != nil {
		f["fibery/meta"] = meta
	}
	return f
}

func moveFixture() map[string]any {
	many := map[string]any{"fibery/collection?": true}
	title := map[string]any{"ui/title?": true}
	src := sdbType("Old/Tasks",
		sdbField("fibery/id", "fibery/uuid"),
		sdbField("fibery/rank", "fibery/rank"),
		sdbField("workflow/state", "workflow/state_Old/Tasks"),
		sdbField("comments/comments", "comments/comment"),
		mvField("Old/Name", "fibery/text", title),
		sdbField("Old/Estimate", "fibery/int"),
		sdbField("Old/Priority", "Old/Priority_Old/Tasks"),
		mvField("Old/Assignee", "fibery/user", many),
		sdbField("Old/Description", "Collaboration~Documents/Document"),
		mvField("Old/Files", "fibery/file", many),
		sdbField("Old/Module", "Old/Modules"),
		sdbField("Old/Sprint", "Old/Sprints"),
		sdbField("Old/Flagged", "fibery/bool"),
		mvField("Old/Score", "fibery/int", map[string]any{"fibery/formula": "x"}),
	)
	src["fibery/id"] = "src-type"
	dst := sdbType("New/Work",
		sdbField("fibery/id", "fibery/uuid"),
		sdbField("workflow/state", "workflow/state_New/Work"),
		sdbField("comments/comments", "comments/comment"),
		mvField("New/Name", "fibery/text", title),
		sdbField("New/Estimate", "fibery/int"),
		sdbField("New/Priority", "New/Priority_New/Work"),
		mvField("New/Assignee", "fibery/user", many),
		sdbField("New/Description", "Collaboration~Documents/Document"),
		mvField("New/Files", "fibery/file", many),
		sdbField("New/Legacy module", "fibery/text"),
		sdbField("New/Flagged", "fibery/text"), // kind differs on purpose
	)
	dst["fibery/id"] = "dst-type"
	mod := sdbType("Old/Modules", mvField("Old/Name", "fibery/text", title))
	return map[string]any{"fibery/types": []any{src, dst, mod}}
}

func TestBuildMovePlan(t *testing.T) {
	p, err := buildMovePlan(moveFixture(), "Old/Tasks", "New/Work", map[string]string{"Old/Module": "New/Legacy module"})
	require.NoError(t, err)
	assert.Equal(t, "Old/Name", p.SrcTitle)
	assert.Equal(t, "New/Name", p.DstTitle)
	assert.True(t, p.Workflow)
	assert.True(t, p.Comments)
	assert.Equal(t, "src-type", p.SrcTypeID)

	kinds := map[string]moveKind{}
	for _, f := range p.Fields {
		kinds[f.Src+"→"+f.Dst] = f.Kind
	}
	assert.Equal(t, map[string]moveKind{
		"Old/Assignee→New/Assignee":       mvUsers,
		"Old/Description→New/Description": mvDoc,
		"Old/Estimate→New/Estimate":       mvPrimitive,
		"Old/Files→New/Files":             mvFiles,
		"Old/Module→New/Legacy module":    mvRelText,
		"Old/Priority→New/Priority":       mvEnum,
	}, kinds)
	for _, f := range p.Fields {
		if f.Kind == mvRelText {
			assert.Equal(t, "Old/Name", f.TitleOf)
		}
	}
	assert.Equal(t, []string{
		"Old/Flagged (fibery/bool → fibery/text: kinds differ)",
		"Old/Sprint (no New/Sprint)",
	}, p.Dropped)
}

func TestBuildMovePlanErrors(t *testing.T) {
	s := moveFixture()
	_, err := buildMovePlan(s, "Old/Tasks", "Old/Tasks", nil)
	assert.Error(t, err)
	_, err = buildMovePlan(s, "Old/Nope", "New/Work", nil)
	assert.Error(t, err)
	_, err = buildMovePlan(s, "Old/Tasks", "New/Work", map[string]string{"Old/Module": "New/Estimate"})
	assert.Error(t, err, "text target must be a text field")
	_, err = buildMovePlan(s, "Old/Tasks", "New/Work", map[string]string{"Old/Nope": "New/Legacy module"})
	assert.Error(t, err)
}

func TestMoveSelectShapes(t *testing.T) {
	p, err := buildMovePlan(moveFixture(), "Old/Tasks", "New/Work", nil)
	require.NoError(t, err)
	sel := moveSelect(p)
	assert.Equal(t, []any{"workflow/state", "enum/name"}, sel["state"])
	assert.Equal(t, []any{"Old/Name"}, sel["title"])
	assert.Contains(t, sel, "comments")
	for i, f := range p.Fields {
		v := sel["f"+string(rune('0'+i))]
		switch f.Kind {
		case mvEnum:
			assert.Equal(t, []any{f.Src, "enum/name"}, v)
		case mvDoc:
			assert.Equal(t, []any{f.Src, "Collaboration~Documents/secret"}, v)
		}
	}
}

func TestDocFileSecrets(t *testing.T) {
	b := []byte(`{"src":"/api/files/4cb507b7-13f8-4183-ae1a-1353271b4cd9","x":"/api/files/4cb507b7-13f8-4183-ae1a-1353271b4cd9?w=1","y":"https://a.fibery.io/api/files/0615462e-0bd7-43b8-b5d8-5f03578b927d"}`)
	assert.Equal(t, []string{"4cb507b7-13f8-4183-ae1a-1353271b4cd9", "0615462e-0bd7-43b8-b5d8-5f03578b927d"}, docFileSecrets(b))
	assert.Empty(t, docFileSecrets([]byte(`{"type":"text"}`)))
}

func TestRemapMentions(t *testing.T) {
	var doc any
	require.NoError(t, json.Unmarshal([]byte(`{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"entity","attrs":{"id":"old-1","typeId":"src-type"}},
		{"type":"entity","attrs":{"id":"old-2","typeId":"src-type"}},
		{"type":"entity","attrs":{"id":"old-1","typeId":"other-type"}}]}]}`), &doc))
	changed := remapMentions(doc, "src-type", "dst-type", map[string]string{"old-1": "new-1"})
	assert.True(t, changed)
	b, _ := json.Marshal(doc)
	assert.Contains(t, string(b), `{"id":"new-1","typeId":"dst-type"}`)
	assert.Contains(t, string(b), `{"id":"old-2","typeId":"src-type"}`, "not moved: left alone")
	assert.Contains(t, string(b), `{"id":"old-1","typeId":"other-type"}`, "other database: left alone")
	assert.False(t, remapMentions(doc, "src-type", "dst-type", map[string]string{}))
}

func TestTargetStateAndPairs(t *testing.T) {
	states := map[string]string{"Released": "r", "To do": "t"}
	id, ok := targetState("Done", map[string]string{"Done": "Released"}, states)
	assert.True(t, ok)
	assert.Equal(t, "r", id)
	id, ok = targetState("To Do", nil, states)
	assert.True(t, ok)
	assert.Equal(t, "t", id)
	_, ok = targetState("Staging", nil, states)
	assert.False(t, ok)

	m, err := parsePairs("x", []string{"A = B", "C=D"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "B", "C": "D"}, m)
	_, err = parsePairs("x", []string{"A"})
	assert.Error(t, err)
	assert.Equal(t, "Old/Tasks #42", legacyIDValue("Old/Tasks", "42"))
}
