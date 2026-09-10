package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateCollectionRouting(t *testing.T) {
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

	// Simulate the routing logic from update.go
	pairs := []string{"Space/Name=Updated title", "Space/Tags=Backend", "Space/Tags=API"}
	fieldMap := map[string][]string{}
	fieldOrder := []string{}
	for _, pair := range pairs {
		k, v, _ := strings.Cut(pair, "=")
		if _, exists := fieldMap[k]; !exists {
			fieldOrder = append(fieldOrder, k)
		}
		fieldMap[k] = append(fieldMap[k], v)
	}

	entity := map[string]any{"fibery/id": "uuid"}
	var collections []collField
	for _, k := range fieldOrder {
		vals := fieldMap[k]
		if len(vals) > 1 || isCollectionField(schema, "Space/Database", k) {
			collections = append(collections, collField{k, vals})
		} else {
			entity[k] = vals[0]
		}
	}

	// Scalar fields go into entity
	assert.Equal(t, "Updated title", entity["Space/Name"])
	// fibery/id stays in entity
	assert.Equal(t, "uuid", entity["fibery/id"])
	// len(entity) > 1 so update call would be made
	assert.Greater(t, len(entity), 1)
	// Tags routes to collections (2 values)
	assert.Len(t, collections, 1)
	assert.Equal(t, "Space/Tags", collections[0].name)
	assert.Equal(t, []string{"Backend", "API"}, collections[0].vals)
}
