package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boardSchema() map[string]any {
	f := func(id, name, typ string, coll bool) map[string]any {
		return map[string]any{"fibery/id": id, "fibery/name": name, "fibery/type": typ, "fibery/meta": map[string]any{"fibery/collection?": coll}}
	}
	return map[string]any{"fibery/types": []any{
		map[string]any{"fibery/id": "t-work", "fibery/name": "Dev/Work", "fibery/fields": []any{
			f("f-state", "workflow/state", "workflow/state_Dev/Work", false),
			f("f-proj", "Dev/Project", "Dev/Project_Dev/Work", false),
			f("f-tags", "Dev/Tags", "Dev/Tags_Dev/Work", true),
			f("f-name", "Dev/Name", "fibery/text", false),
		}},
		map[string]any{"fibery/id": "t-state", "fibery/name": "workflow/state_Dev/Work"},
		map[string]any{"fibery/id": "t-proj", "fibery/name": "Dev/Project_Dev/Work"},
		map[string]any{"fibery/id": "t-tags", "fibery/name": "Dev/Tags_Dev/Work"},
	}}
}

func TestBoardAxis(t *testing.T) {
	s := boardSchema()
	id, tid, err := boardAxis(s, "Dev/Work", "workflow/state")
	require.NoError(t, err)
	assert.Equal(t, "f-state", id)
	assert.Equal(t, "t-state", tid)
	id, _, err = boardAxis(s, "Dev/Work", "Project") // bare name gets the space
	require.NoError(t, err)
	assert.Equal(t, "f-proj", id)
	_, _, err = boardAxis(s, "Dev/Work", "Tags")
	assert.ErrorContains(t, err, "collection")
	_, _, err = boardAxis(s, "Dev/Work", "Name")
	assert.ErrorContains(t, err, "must be a single-select")
	_, _, err = boardAxis(s, "Dev/Work", "Nope")
	assert.ErrorContains(t, err, "not found")
}

func TestBuildViewRecord_Board(t *testing.T) {
	rec := buildViewRecord(viewSpec{ID: "v1", Name: "B", Type: "board", DBType: "t-work", AppID: "a1",
		XField: "f-state", XType: "t-state", YField: "f-proj", YType: "t-proj"})
	meta := rec["fibery/meta"].(map[string]any)
	assert.Equal(t, "t-state", meta["x"].(map[string]any)["query"].(map[string]any)["q/from"])
	assert.Equal(t, "t-proj", meta["y"].(map[string]any)["query"].(map[string]any)["q/from"])
	item := meta["items"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{"f-state"}, item["xExpression"])
	assert.Equal(t, []any{"f-proj"}, item["yExpression"])

	list := buildViewRecord(viewSpec{ID: "v2", Name: "L", Type: "list", DBType: "t-work", AppID: "a1"})
	assert.NotContains(t, list["fibery/meta"].(map[string]any), "x")
}

func TestViewDBNames(t *testing.T) {
	s := boardSchema()
	raw, _ := json.Marshal(map[string]any{"items": []any{
		map[string]any{"query": map[string]any{"q/from": "t-work"}},
		map[string]any{"query": map[string]any{"q/from": "gone"}},
	}})
	names, dead := viewDBNames(s, raw)
	assert.Equal(t, []string{"<deleted>", "Dev/Work"}, names)
	assert.False(t, dead)

	raw, _ = json.Marshal(map[string]any{"items": []any{map[string]any{"query": map[string]any{"q/from": "gone"}}}})
	_, dead = viewDBNames(s, raw)
	assert.True(t, dead)

	_, dead = viewDBNames(s, json.RawMessage(`{"documentSecret":"x"}`))
	assert.False(t, dead, "a document reads no database — never dead")
}
