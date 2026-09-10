package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var emptySchema = map[string]any{"fibery/types": []any{}}

func TestCollectDocFields_Inline(t *testing.T) {
	got, err := collectDocFields(emptySchema, "Space/DB", []string{`Space/Desc=line1\nline2`}, nil)
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "Space/Desc", got[0].field)
	assert.Equal(t, "line1\nline2", got[0].content) // \n escape interpreted
}

func TestCollectDocFields_File(t *testing.T) {
	p := filepath.Join(t.TempDir(), "body.md")
	assert.NoError(t, os.WriteFile(p, []byte("# Title\n\nbody\n"), 0600))
	got, err := collectDocFields(emptySchema, "Space/DB", nil, []string{"Space/Desc=" + p})
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "# Title\n\nbody\n", got[0].content) // file content verbatim
}

func TestCollectDocFields_MissingFile(t *testing.T) {
	_, err := collectDocFields(emptySchema, "Space/DB", nil, []string{"Space/Desc=/no/such/file.md"})
	assert.Error(t, err)
}

func TestCollectDocFields_BadPair(t *testing.T) {
	_, err := collectDocFields(emptySchema, "Space/DB", []string{"noEquals"}, nil)
	assert.Error(t, err)
}

func TestCreateFieldGrouping(t *testing.T) {
	pairs := []string{"Space/Name=foo", "Space/Tags=Backend", "Space/Tags=API"}
	fieldMap := map[string][]string{}
	fieldOrder := []string{}
	for _, pair := range pairs {
		k, v, _ := strings.Cut(pair, "=")
		if _, exists := fieldMap[k]; !exists {
			fieldOrder = append(fieldOrder, k)
		}
		fieldMap[k] = append(fieldMap[k], v)
	}
	assert.Equal(t, []string{"Space/Name", "Space/Tags"}, fieldOrder)
	assert.Equal(t, []string{"Backend", "API"}, fieldMap["Space/Tags"])
	assert.Equal(t, []string{"foo"}, fieldMap["Space/Name"])
}

func TestCreateIDExtraction(t *testing.T) {
	resultJSON := `{"fibery/id":"550e8400-e29b-41d4-a716-446655440000","Space/Name":"test"}`
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(resultJSON), &m)
	id := strings.Trim(string(m["fibery/id"]), `"`)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", id)
}

func TestCreateCollectionRouting(t *testing.T) {
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
					map[string]any{
						"fibery/name": "Space/Name",
						"fibery/type": "fibery/text",
						"fibery/meta": map[string]any{},
					},
				},
			},
		},
	}

	type collField struct {
		name string
		vals []string
	}

	fieldMap := map[string][]string{
		"Space/Name": {"My item"},
		"Space/Tags": {"Backend", "API"},
	}
	fieldOrder := []string{"Space/Name", "Space/Tags"}

	entity := map[string]any{}
	var collections []collField
	for _, k := range fieldOrder {
		vals := fieldMap[k]
		if len(vals) > 1 || isCollectionField(schema, "Space/Database", k) {
			collections = append(collections, collField{k, vals})
		} else {
			entity[k] = vals[0]
		}
	}

	// Space/Name is scalar (1 value, not a collection field)
	assert.Equal(t, map[string]any{"Space/Name": "My item"}, entity)
	// Space/Tags routes to collections (2 values)
	assert.Len(t, collections, 1)
	assert.Equal(t, "Space/Tags", collections[0].name)
	assert.Equal(t, []string{"Backend", "API"}, collections[0].vals)
}
