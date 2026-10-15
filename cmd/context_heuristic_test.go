package cmd

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEdgeRole(t *testing.T) {
	cases := []struct {
		name       string
		collection bool
		want       string
	}{
		{"Development/Dev epic", false, "parent"},
		{"Development/Story", false, "parent"},
		{"Development/Dev Tasks", true, "children"},
		{"Development/Blocked By", false, "blockers"},
		{"Development/Related To", false, "blockers"},
		{"GitLab/Merge Requests (GitLab - Dev Task)", true, "code"},
		{"Development/bugs", true, "quality"},
		{"Development/Task bugs", true, "quality"},
		{"Development/Services", false, "service"},
		{"Development/effort", false, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, edgeRole(c.name, c.collection), c.name)
	}
}

func TestClassifyProfile(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Development/Dev Task",
				"fibery/fields": []any{
					field("Development/name", "fibery/text", map[string]any{"ui/title?": true}),
					field("workflow/state", "workflow/state_Development/Dev Task", nil),
					field("Development/Priority", "Development/Priority_Software Development/User Story", nil),
					field("assignments/assignees", "fibery/user", map[string]any{"fibery/collection?": true}),
					field("fibery/created-by", "fibery/user", nil),
					field("fibery/creation-date", "fibery/date-time", nil),
					field("Development/description", "Collaboration~Documents/Document", nil),
					field("Development/Knowledge transfer", "Collaboration~Documents/Document", nil),
					field("Development/Story Description", "Collaboration~Documents/Document", nil), // mirror → drop
					field("Development/Count Open MRs", "fibery/int", nil),                          // rollup → drop
					field("Development/Flagged", "fibery/bool", nil),                                // flag → drop
					field("Development/Date Of In Progress", "fibery/date-time", nil),               // transition → drop
					field("Development/Dev epic", "Development/Dev epic", nil),                      // edge parent
					field("Development/bugs", "Development/bug", map[string]any{"fibery/collection?": true}), // edge quality
				},
			},
		},
	}
	prof, unclassified := classifyProfile(schema, "Development/Dev Task")

	assert.Contains(t, prof.Keep, "Development/name")
	assert.Contains(t, prof.Keep, "workflow/state")
	assert.Contains(t, prof.Keep, "Development/Priority")
	assert.Contains(t, prof.Keep, "assignments/assignees")
	assert.Contains(t, prof.Keep, "fibery/created-by")
	assert.Contains(t, prof.Keep, "fibery/creation-date")

	assert.Contains(t, prof.Docs, "Development/description")
	assert.Contains(t, prof.Docs, "Development/Knowledge transfer")
	assert.NotContains(t, prof.Docs, "Development/Story Description")

	assert.NotContains(t, prof.Keep, "Development/Count Open MRs")
	assert.NotContains(t, prof.Keep, "Development/Flagged")
	assert.NotContains(t, prof.Keep, "Development/Date Of In Progress")

	assert.Equal(t, []string{"Development/Dev epic"}, prof.Edges.Parent)
	assert.Equal(t, []string{"Development/bugs"}, prof.Edges.Quality)
	assert.Equal(t, "full", prof.Comments)

	sort.Strings(unclassified)
	assert.Empty(t, unclassified)
}

// field is a test helper building one schema field entry.
func field(name, typ string, meta map[string]any) map[string]any {
	if meta == nil {
		meta = map[string]any{}
	}
	return map[string]any{"fibery/name": name, "fibery/type": typ, "fibery/meta": meta}
}
