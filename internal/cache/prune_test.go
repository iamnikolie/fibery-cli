package cache_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tombstoneSchema mirrors the shape a real workspace returns after fields and a
// database have been deleted in the Fibery UI.
func tombstoneSchema() map[string]any {
	return map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name":     "Launch Kanban/Person_039gf2b_deleted",
				"fibery/deleted?": true,
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Launch Kanban/Name", "fibery/type": "fibery/text", "fibery/deleted?": true},
				},
			},
			map[string]any{
				"fibery/name":     "Launch Kanban/Issue",
				"fibery/deleted?": false,
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Launch Kanban/Name", "fibery/type": "fibery/text", "fibery/deleted?": false},
					map[string]any{"fibery/name": "Launch Kanban/User_1oddzk6_deleted", "fibery/type": "Launch Kanban/Person_039gf2b_deleted", "fibery/deleted?": true},
					// Live field pointing at a type that was deleted.
					map[string]any{"fibery/name": "Launch Kanban/Owner", "fibery/type": "Launch Kanban/Person_039gf2b_deleted", "fibery/deleted?": false},
					// Live field whose name merely ends in "_deleted".
					map[string]any{"fibery/name": "Launch Kanban/Reason_deleted", "fibery/type": "fibery/text", "fibery/deleted?": false},
				},
			},
			// Pre-flag schema: no fibery/deleted? key at all.
			map[string]any{
				"fibery/name": "Launch Kanban/Legacy",
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Launch Kanban/Title", "fibery/type": "fibery/text"},
				},
			},
		},
	}
}

func typeNames(t *testing.T, schema map[string]any) []string {
	t.Helper()
	types, ok := schema["fibery/types"].([]any)
	require.True(t, ok)
	var out []string
	for _, ty := range types {
		tm, ok := ty.(map[string]any)
		require.True(t, ok)
		out = append(out, tm["fibery/name"].(string))
	}
	return out
}

func fieldNames(t *testing.T, schema map[string]any, db string) []string {
	t.Helper()
	types, _ := schema["fibery/types"].([]any)
	for _, ty := range types {
		tm, _ := ty.(map[string]any)
		if tm["fibery/name"] != db {
			continue
		}
		fields, _ := tm["fibery/fields"].([]any)
		var out []string
		for _, f := range fields {
			fm, _ := f.(map[string]any)
			out = append(out, fm["fibery/name"].(string))
		}
		return out
	}
	t.Fatalf("database %q not found in pruned schema", db)
	return nil
}

func TestLoadSchema_PrunesDeleted(t *testing.T) {
	t.Setenv("FIBERY_HOME", t.TempDir())
	require.NoError(t, cache.SaveSchema(tombstoneSchema(), ""))

	loaded, err := cache.LoadSchema("")
	require.NoError(t, err)

	assert.Equal(t, []string{"Launch Kanban/Issue", "Launch Kanban/Legacy"}, typeNames(t, loaded),
		"deleted type must not survive the load")

	assert.Equal(t, []string{"Launch Kanban/Name", "Launch Kanban/Reason_deleted"}, fieldNames(t, loaded, "Launch Kanban/Issue"),
		"deleted fields and relations to a deleted type must go; a live field named *_deleted must stay")

	assert.Equal(t, []string{"Launch Kanban/Title"}, fieldNames(t, loaded, "Launch Kanban/Legacy"),
		"a schema without the fibery/deleted? flag must pass through untouched")
}

// The cache file stays a faithful copy of the server response, so the tombstones
// remain inspectable on disk even though no command ever sees them.
func TestSaveSchema_KeepsDeletedOnDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FIBERY_HOME", home)
	require.NoError(t, cache.SaveSchema(tombstoneSchema(), ""))

	raw, err := os.ReadFile(filepath.Join(home, "schema.json"))
	require.NoError(t, err)

	var onDisk map[string]any
	require.NoError(t, json.Unmarshal(raw, &onDisk))
	assert.Len(t, onDisk["fibery/types"], 3)
}
