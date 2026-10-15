package cmd

import (
	"encoding/json"
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// viewTestSchema carries field ids, which views store instead of names.
var viewTestSchema = map[string]any{
	"fibery/types": []any{
		map[string]any{
			"fibery/name": "Space/Bug",
			"fibery/id":   "bug-type",
			"fibery/fields": []any{
				map[string]any{"fibery/name": "Space/Name", "fibery/id": "f-name", "fibery/type": "fibery/text"},
				map[string]any{"fibery/name": "Space/Flagged", "fibery/id": "f-flag", "fibery/type": "fibery/bool"},
				map[string]any{"fibery/name": "workflow/state", "fibery/id": "f-state", "fibery/type": "workflow/state_Space/Bug"},
				map[string]any{"fibery/name": "assignments/assignees", "fibery/id": "f-assignees", "fibery/type": "fibery/user",
					"fibery/meta": map[string]any{"fibery/collection?": true}},
				map[string]any{"fibery/name": "fibery/creation-date", "fibery/id": "f-created", "fibery/type": "fibery/date-time"},
			},
		},
		map[string]any{
			"fibery/name": "workflow/state_Space/Bug",
			"fibery/fields": []any{
				map[string]any{"fibery/name": "fibery/id", "fibery/id": "f-state-id", "fibery/type": "fibery/uuid"},
				map[string]any{"fibery/name": "enum/name", "fibery/id": "f-state-name", "fibery/type": "fibery/text"},
			},
		},
	},
}

func noResolve(schemaField, string) (string, error) { return "", nil }

func TestBuildViewFilters_TextContainsUsesFieldIDs(t *testing.T) {
	got, err := buildViewFilters(viewTestSchema, "Space/Bug", []string{"space/name~[ii]"}, noResolve)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []any{"q/contains", []any{"f-name"}, "$where1"}, got[0].Expression)
	assert.Equal(t, map[string]any{"$where1": "[ii]"}, got[0].Params)
}

func TestBuildViewFilters_EnumMatchesByResolvedID(t *testing.T) {
	resolve := func(f schemaField, v string) (string, error) {
		assert.Equal(t, "workflow/state", f.Name)
		assert.Equal(t, "Open", v)
		return "state-open-uuid", nil
	}
	got, err := buildViewFilters(viewTestSchema, "Space/Bug", []string{"Space/Name=x", "workflow/state!=Open"}, resolve)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []any{"q/not-in", []any{"f-state", "f-state-id"}, "$where2"}, got[1].Expression)
	assert.Equal(t, map[string]any{"$where2": []any{"state-open-uuid"}}, got[1].Params)
}

func TestBuildViewFilters_Rejects(t *testing.T) {
	cases := map[string]string{
		"collection":      "assignments/assignees=a@b.c",
		"contains on ref": "workflow/state~Op",
		"unknown field":   "Space/Nope=1",
		"no operator":     "Space/Name",
		"bad bool":        "Space/Flagged=maybe",
	}
	for name, f := range cases {
		_, err := buildViewFilters(viewTestSchema, "Space/Bug", []string{f}, noResolve)
		assert.Error(t, err, name)
	}
}

func TestTranslateWhereNames(t *testing.T) {
	where := []any{"and",
		[]any{"=", []any{"workflow/state", "enum/name"}, "$s"},
		[]any{"q/contains", []any{"Space/Name"}, "$n"},
	}
	got, err := translateWhereNames(viewTestSchema, "Space/Bug", where)
	require.NoError(t, err)
	assert.Equal(t, []any{"and",
		[]any{"=", []any{"f-state", "f-state-name"}, "$s"},
		[]any{"q/contains", []any{"f-name"}, "$n"},
	}, got)

	_, err = translateWhereNames(viewTestSchema, "Space/Bug", []any{"=", []any{"workflow/state", "enum/nope"}, "$s"})
	assert.Error(t, err)
}

func TestBuildViewRecord_EmbeddedAndFiltered(t *testing.T) {
	f1 := viewFilter{Expression: []any{"q/contains", []any{"f-name"}, "$where1"}, Params: map[string]any{"$where1": "[ii]"}}
	f2 := viewFilter{Expression: []any{"=", []any{"f-flag"}, "$where2"}, Params: map[string]any{"$where2": true}}
	rec := buildViewRecord(viewSpec{
		ID: "view-1", Name: "ii", Type: "table", DBType: "bug-type", AppID: "app-1",
		Filters: []viewFilter{f1, f2}, AnyOf: true,
		Units:   defaultViewUnits(viewTestSchema, "Space/Bug"),
		OrderBy: defaultViewOrder(viewTestSchema, "Space/Bug"),
		Owner:   &viewOwner{TypeID: "bug-type", UUID: "owner-uuid", PublicID: "3947"},
	})

	assert.Equal(t, "object", rec["fibery/container-type"])
	assert.Equal(t, "3947", rec["fibery/container-entity-id"])
	assert.Equal(t, "owner-uuid", rec["fibery/container-entity-uid"])
	assert.Equal(t, map[string]any{"fibery/id": "bug-type"}, rec["fibery/container-entity-type"])

	item := rec["fibery/meta"].(map[string]any)["items"].([]any)[0].(map[string]any)
	query := item["query"].(map[string]any)
	assert.Equal(t, "bug-type", query["q/from"])
	assert.Equal(t, []any{"or", f1.Expression, f2.Expression}, query["q/where"])
	assert.Equal(t, []any{[]any{[]any{"f-created"}, "q/desc"}}, query["q/order-by"])
	assert.Equal(t, map[string]any{"$where1": "[ii]", "$where2": true}, item["params"])

	filter := item["filter"].(map[string]any)
	assert.Equal(t, "group", filter["kind"])
	assert.Equal(t, "or", filter["operator"])
	assert.Len(t, filter["value"], 2)

	units := item["units"].([]any)
	assert.Len(t, units, 3, "title, workflow state, id")
}

func TestBuildViewRecord_LeftNavWithoutFilters(t *testing.T) {
	rec := buildViewRecord(viewSpec{ID: "v", Name: "all", Type: "list", DBType: "bug-type", AppID: "app-1"})
	assert.Equal(t, "app", rec["fibery/container-type"])
	assert.NotContains(t, rec, "fibery/container-entity-id")
	item := rec["fibery/meta"].(map[string]any)["items"].([]any)[0].(map[string]any)
	assert.NotContains(t, item["query"], "q/where")
	assert.NotContains(t, item, "filter")
}

// sizedDoc: heading "Scope" (size 7), paragraph "ab" (4), heading "Next" (6), empty paragraph (2).
const sizedDoc = `{"type":"doc","content":[` +
	`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Scope"}]},` +
	`{"type":"paragraph","content":[{"type":"text","text":"ab"}]},` +
	`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Next"}]},` +
	`{"type":"paragraph"}]}`

func topTypes(t *testing.T, doc json.RawMessage) []string {
	_, _, nodes, err := parseDocNodes(doc)
	require.NoError(t, err)
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n["type"].(string)
	}
	return out
}

func TestInsertDocBlock_AfterHeadingSectionShiftsLaterComments(t *testing.T) {
	content := client.DocContent{
		Doc: json.RawMessage(sizedDoc),
		Comments: []any{
			map[string]any{"from": float64(8), "to": float64(10)},  // inside "ab" — before the insert
			map[string]any{"from": float64(12), "to": float64(16)}, // inside "Next" — after it
		},
	}
	require.NoError(t, insertDocBlock(&content, viewEmbedNode("view-1"), "scope"))

	assert.Equal(t, []string{"heading", "paragraph", "fiberyView", "heading", "paragraph"}, topTypes(t, content.Doc))
	assert.Equal(t, float64(8), content.Comments[0].(map[string]any)["from"])
	assert.Equal(t, float64(13), content.Comments[1].(map[string]any)["from"])
	assert.Equal(t, float64(17), content.Comments[1].(map[string]any)["to"])

	err := insertDocBlock(&content, viewEmbedNode("view-1"), "")
	assert.ErrorContains(t, err, "already embedded")
}

func TestInsertDocBlock_EndAndEmptyDoc(t *testing.T) {
	content := client.DocContent{Doc: json.RawMessage(sizedDoc)}
	require.NoError(t, insertDocBlock(&content, viewEmbedNode("v"), ""))
	assert.Equal(t, "fiberyView", topTypes(t, content.Doc)[4])

	empty := client.DocContent{}
	require.NoError(t, insertDocBlock(&empty, viewEmbedNode("v"), ""))
	assert.Equal(t, []string{"fiberyView"}, topTypes(t, empty.Doc))
}

func TestInsertDocBlock_KeepsUntouchedNodesByteIdentical(t *testing.T) {
	content := client.DocContent{Doc: json.RawMessage(sizedDoc)}
	require.NoError(t, insertDocBlock(&content, viewEmbedNode("v"), ""))
	assert.Contains(t, string(content.Doc), `{"type":"paragraph","content":[{"type":"text","text":"ab"}]}`)
}

func TestCheckInsertPoint(t *testing.T) {
	assert.NoError(t, checkInsertPoint(json.RawMessage(sizedDoc), "Next"))
	assert.ErrorContains(t, checkInsertPoint(json.RawMessage(sizedDoc), "Missing"), "no heading")
}

func TestFilterViews(t *testing.T) {
	views := []client.ViewRecord{
		{Name: "doc", Type: "document", ContainerAppID: "a1"},
		{Name: "ii", Type: "table", ContainerAppID: "a1", ContainerEntityID: "3947"},
		{Name: "board", Type: "board", ContainerAppID: "a2"},
	}
	apps := map[string]string{"a1": "Development", "a2": "QA"}
	assert.Len(t, filterViews(views, apps, "", "", ""), 2)
	assert.Len(t, filterViews(views, apps, "development", "", ""), 1)
	assert.Len(t, filterViews(views, apps, "", "board", ""), 1)
	got := filterViews(views, apps, "", "", "3947")
	require.Len(t, got, 1)
	assert.Equal(t, "ii", got[0]["name"])
}

func TestRemoveViewEmbeds_RoundTripsInsert(t *testing.T) {
	content := client.DocContent{
		Doc:      json.RawMessage(sizedDoc),
		Comments: []any{map[string]any{"from": float64(12), "to": float64(16)}},
	}
	require.NoError(t, insertDocBlock(&content, viewEmbedNode("v"), "Scope"))
	n, err := removeViewEmbeds(&content, "v")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.JSONEq(t, sizedDoc, string(content.Doc))
	assert.Equal(t, float64(12), content.Comments[0].(map[string]any)["from"])

	n, err = removeViewEmbeds(&content, "v")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
