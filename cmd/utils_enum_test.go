package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFieldValue_PlainJSON(t *testing.T) {
	v, err := parseFieldValue(`{"fibery/id":"abc"}`)
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{"fibery/id": "abc"}, v)
}

func TestParseFieldValue_UUID(t *testing.T) {
	v, err := parseFieldValue("550e8400-e29b-41d4-a716-446655440000")
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{"fibery/id": "550e8400-e29b-41d4-a716-446655440000"}, v)
}

func TestParseFieldValue_PlainString(t *testing.T) {
	v, err := parseFieldValue("hello world")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", v)
}

func TestFindFieldType_ReturnsEmptyWhenNotFound(t *testing.T) {
	schema := map[string]any{"fibery/types": []any{}}
	assert.Equal(t, "", findFieldType(schema, "Space/DB", "Space/Field"))
}

func TestFindFieldType_FindsField(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Space/DB",
				"fibery/fields": []any{
					map[string]any{
						"fibery/name": "Space/Priority",
						"fibery/type": "Space/Priority_Space/DB",
					},
				},
			},
		},
	}
	assert.Equal(t, "Space/Priority_Space/DB", findFieldType(schema, "Space/DB", "Space/Priority"))
}

func TestIsCollectionField_True(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Space/Database",
				"fibery/fields": []any{
					map[string]any{
						"fibery/name": "Space/Tags",
						"fibery/type": "Space/Tags_Space/Database",
						"fibery/meta": map[string]any{"fibery/collection?": true},
					},
				},
			},
		},
	}
	assert.True(t, isCollectionField(schema, "Space/Database", "Space/Tags"))
}

func TestIsCollectionField_False(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Space/Database",
				"fibery/fields": []any{
					map[string]any{
						"fibery/name": "Space/Priority",
						"fibery/type": "Space/Priority_Space/Database",
						"fibery/meta": map[string]any{},
					},
				},
			},
		},
	}
	assert.False(t, isCollectionField(schema, "Space/Database", "Space/Priority"))
}

func TestIsCollectionField_FieldNotFound(t *testing.T) {
	schema := map[string]any{"fibery/types": []any{}}
	assert.False(t, isCollectionField(schema, "Space/Database", "Space/Tags"))
}

func TestUnescapeDocContent_Newlines(t *testing.T) {
	// User types --doc "Field=line1\nline2" — the shell delivers a literal
	// backslash-n. Without interpretation the document gets "\n" as text.
	got := unescapeDocContent(`line1\nline2`)
	assert.Equal(t, "line1\nline2", got)
}

func TestUnescapeDocContent_TabsAndCRs(t *testing.T) {
	got := unescapeDocContent(`col1\tcol2\rrest`)
	assert.Equal(t, "col1\tcol2\rrest", got)
}

func TestUnescapeDocContent_BackslashEscape(t *testing.T) {
	// "\\n" should stay a literal backslash-n in the document (escape hatch).
	got := unescapeDocContent(`literal\\nbackslash`)
	assert.Equal(t, `literal\nbackslash`, got)
}

func TestUnescapeDocContent_NoEscapes(t *testing.T) {
	got := unescapeDocContent("plain text")
	assert.Equal(t, "plain text", got)
}

func TestResolveFieldName_ExactMatch(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Development/Dev Task",
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Development/Name", "fibery/type": "fibery/text"},
				},
			},
		},
	}
	assert.Equal(t, "Development/Name",
		resolveFieldName(schema, "Development/Dev Task", "Development/Name"))
}

func TestResolveFieldName_CaseInsensitiveLowercase(t *testing.T) {
	// User types Development/name (lowercase n) — schema has Development/Name.
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Development/Dev Task",
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Development/Name", "fibery/type": "fibery/text"},
				},
			},
		},
	}
	assert.Equal(t, "Development/Name",
		resolveFieldName(schema, "Development/Dev Task", "Development/name"))
}

func TestResolveFieldName_CaseInsensitiveUppercase(t *testing.T) {
	// User types Support platform/Name — schema has Support platform/name.
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Support platform/Ticket",
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Support platform/name", "fibery/type": "fibery/text"},
				},
			},
		},
	}
	assert.Equal(t, "Support platform/name",
		resolveFieldName(schema, "Support platform/Ticket", "Support platform/Name"))
}

func TestResolveFieldName_NotFoundReturnsInput(t *testing.T) {
	schema := map[string]any{"fibery/types": []any{}}
	// Unknown fields pass through unchanged so the API can produce its own error.
	assert.Equal(t, "Space/Unknown",
		resolveFieldName(schema, "Space/DB", "Space/Unknown"))
}
