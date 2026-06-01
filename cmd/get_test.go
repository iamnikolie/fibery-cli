package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetUsesFullSelect(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Space/Database",
				"fibery/fields": []any{
					map[string]any{
						"fibery/name": "fibery/id",
						"fibery/type": "fibery/uuid",
						"fibery/meta": map[string]any{"fibery/id?": true},
					},
					map[string]any{
						"fibery/name": "fibery/public-id",
						"fibery/type": "fibery/text",
						"fibery/meta": map[string]any{},
					},
					map[string]any{
						"fibery/name": "fibery/creation-date",
						"fibery/type": "fibery/date-time",
						"fibery/meta": map[string]any{},
					},
					map[string]any{
						"fibery/name": "workflow/state",
						"fibery/type": "workflow/state_Space/Database",
						"fibery/meta": map[string]any{},
					},
					map[string]any{
						"fibery/name": "Space/Name",
						"fibery/type": "fibery/text",
						"fibery/meta": map[string]any{"ui/title?": true},
					},
					map[string]any{
						"fibery/name": "Space/Priority",
						"fibery/type": "Space/Priority_Space/Database",
						"fibery/meta": map[string]any{},
					},
					map[string]any{
						"fibery/name": "Space/Score",
						"fibery/type": "fibery/int",
						"fibery/meta": map[string]any{},
					},
				},
			},
		},
	}
	fullSel, _ := buildFullSelect(schema, "Space/Database")
	minSel := buildSelect(schema, "Space/Database")

	// buildFullSelect must return more fields than the minimal select
	assert.Greater(t, len(fullSel), len(minSel),
		"full select should include Priority and Score; minimal select has only %d keys", len(minSel))
}

func TestExtractFiberyID_Present(t *testing.T) {
	raw := []byte(`{"fibery/id":"550e8400-e29b-41d4-a716-446655440000","Name":"x"}`)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", extractFiberyID(raw))
}

func TestExtractFiberyID_Missing(t *testing.T) {
	raw := []byte(`{"Name":"x"}`)
	assert.Equal(t, "", extractFiberyID(raw))
}

func TestExtractFiberyID_NotJSON(t *testing.T) {
	assert.Equal(t, "", extractFiberyID([]byte(`not json`)))
}

func TestNormalizeEntityRef_UUIDPassthrough(t *testing.T) {
	got, isUUID := normalizeEntityRef("550e8400-e29b-41d4-a716-446655440000")
	assert.True(t, isUUID)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", got)
}

func TestNormalizeEntityRef_PrefixStripped(t *testing.T) {
	got, isUUID := normalizeEntityRef("DT-42")
	assert.False(t, isUUID)
	assert.Equal(t, "42", got)
}

func TestNormalizeEntityRef_PlainPublicID(t *testing.T) {
	got, isUUID := normalizeEntityRef("42")
	assert.False(t, isUUID)
	assert.Equal(t, "42", got)
}

func TestNormalizeEntityRef_OtherInput(t *testing.T) {
	// Unknown format — pass through; the lookup will fail with a useful error.
	got, isUUID := normalizeEntityRef("anything")
	assert.False(t, isUUID)
	assert.Equal(t, "anything", got)
}

func TestFilterSelectByAliases_EmptyAliasesUnchanged(t *testing.T) {
	sel := map[string]any{
		"fibery/id": []any{"fibery/id"},
		"Name":      []any{"Space/Name"},
		"Priority":  []any{"Space/Priority", "enum/name"},
	}
	out, err := filterSelectByAliases(sel, nil, "fibery/id")
	assert.NoError(t, err)
	assert.Equal(t, sel, out)
}

func TestFilterSelectByAliases_KeepsRequestedPlusAlwaysKeep(t *testing.T) {
	sel := map[string]any{
		"fibery/id": []any{"fibery/id"},
		"Public ID": []any{"fibery/public-id"},
		"Name":      []any{"Space/Name"},
		"Priority":  []any{"Space/Priority", "enum/name"},
		"Tags":      []any{"Space/Tags", "enum/name"},
	}
	out, err := filterSelectByAliases(sel, []string{"Priority"}, "fibery/id", "Public ID", "Name")
	assert.NoError(t, err)
	assert.Contains(t, out, "fibery/id")
	assert.Contains(t, out, "Public ID")
	assert.Contains(t, out, "Name")
	assert.Contains(t, out, "Priority")
	assert.NotContains(t, out, "Tags")
}

func TestFilterSelectByAliases_CaseInsensitive(t *testing.T) {
	sel := map[string]any{
		"Priority": []any{"Space/Priority", "enum/name"},
	}
	out, err := filterSelectByAliases(sel, []string{"priority"})
	assert.NoError(t, err)
	assert.Contains(t, out, "Priority")
}

func TestFilterSelectByAliases_DocKeysMatchedByStrippedName(t *testing.T) {
	// _doc_Description should match the user typing "Description".
	sel := map[string]any{
		"Name":             []any{"Space/Name"},
		"_doc_Description": []any{"Space/Description", "Collaboration~Documents/secret"},
	}
	out, err := filterSelectByAliases(sel, []string{"Description"})
	assert.NoError(t, err)
	assert.Contains(t, out, "_doc_Description")
	assert.NotContains(t, out, "Name")
}

func TestFilterSelectByAliases_UnknownFieldErrors(t *testing.T) {
	sel := map[string]any{"Name": []any{"Space/Name"}}
	_, err := filterSelectByAliases(sel, []string{"DoesNotExist"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DoesNotExist")
}

func TestLooksLikeEntityRef(t *testing.T) {
	assert.True(t, looksLikeEntityRef("42"))
	assert.True(t, looksLikeEntityRef("DT-42"))
	assert.True(t, looksLikeEntityRef("550e8400-e29b-41d4-a716-446655440000"))
	assert.False(t, looksLikeEntityRef("some-random-secret-abc"))
	assert.False(t, looksLikeEntityRef("hello world"))
}

func TestBuildFullSelect_IncludesFiberyID(t *testing.T) {
	// fibery/id is marked with fibery/id?: true in the schema, which causes the
	// iteration to skip it. Make sure we still inject the UUID into the select.
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Space/Database",
				"fibery/fields": []any{
					map[string]any{
						"fibery/name": "fibery/id",
						"fibery/type": "fibery/uuid",
						"fibery/meta": map[string]any{"fibery/id?": true},
					},
					map[string]any{
						"fibery/name": "fibery/public-id",
						"fibery/type": "fibery/text",
						"fibery/meta": map[string]any{"fibery/id?": true},
					},
					map[string]any{
						"fibery/name": "Space/Name",
						"fibery/type": "fibery/text",
						"fibery/meta": map[string]any{"ui/title?": true},
					},
				},
			},
		},
	}
	sel, _ := buildFullSelect(schema, "Space/Database")
	assert.Equal(t, []any{"fibery/id"}, sel["fibery/id"],
		"buildFullSelect must include 'fibery/id' so --json output carries the UUID")
	assert.Equal(t, []any{"fibery/public-id"}, sel["Public ID"],
		"buildFullSelect must include 'Public ID' for the human-readable ID")
}
