package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func sdbField(name, typ string) map[string]any {
	return map[string]any{"fibery/name": name, "fibery/type": typ}
}

func sdbType(name string, fields ...map[string]any) map[string]any {
	fs := make([]any, len(fields))
	for i, f := range fields {
		fs[i] = f
	}
	return map[string]any{"fibery/name": name, "fibery/fields": fs, "fibery/meta": map[string]any{}}
}

func refsFixture() map[string]any {
	return map[string]any{"fibery/types": []any{
		sdbType("Space/Bugs",
			sdbField("Space/Name", "fibery/text"),
			sdbField("Space/Priority", "Space/Priority_Space/Bugs"),
			sdbField("workflow/state", "workflow/state_Space/Bugs"),
			sdbField("Space/Task", "Space/Tasks"),
			sdbField("Space/Assignees", "fibery/user"),
		),
		sdbType("Space/Tasks",
			sdbField("Space/Name", "fibery/text"),
			sdbField("Space/Bugs", "Space/Bugs"),
		),
		sdbType("Space/Priority_Space/Bugs", sdbField("Space/entities", "Space/Bugs")),
		sdbType("workflow/state_Space/Bugs", sdbField("workflow/objects", "Space/Bugs")),
		sdbType("Space/Priority_Space/Tasks", sdbField("Space/entities", "Space/Tasks")),
		sdbType("fibery/user", sdbField("user/Bugs", "Space/Bugs")),
	}}
}

func TestDBComponents(t *testing.T) {
	got := dbComponents(refsFixture(), "Space/Bugs")
	assert.Equal(t, []string{"Space/Priority_Space/Bugs", "workflow/state_Space/Bugs"}, got)
}

func TestDBComponents_NoneForOtherDB(t *testing.T) {
	got := dbComponents(refsFixture(), "Space/Tasks")
	assert.Equal(t, []string{"Space/Priority_Space/Tasks"}, got)
}

func TestDBRefs(t *testing.T) {
	s := refsFixture()
	out, in := dbRefs(s, "Space/Bugs")
	// listDatabases skips types whose meta lacks the platform flag only by name pattern;
	// fibery/user is always a known target.
	assert.Contains(t, out, schemaRef{Holder: "Space/Bugs", Field: "Space/Task", Target: "Space/Tasks"})
	assert.Contains(t, out, schemaRef{Holder: "Space/Bugs", Field: "Space/Assignees", Target: "fibery/user"})
	assert.NotContains(t, out, schemaRef{Holder: "Space/Bugs", Field: "Space/Priority", Target: "Space/Priority_Space/Bugs"})

	assert.Contains(t, in, schemaRef{Holder: "Space/Tasks", Field: "Space/Bugs", Target: "Space/Bugs"})
	assert.Contains(t, in, schemaRef{Holder: "fibery/user", Field: "user/Bugs", Target: "Space/Bugs"})
	for _, r := range in {
		assert.NotEqual(t, "Space/Priority_Space/Bugs", r.Holder, "own enum is not an incoming ref")
		assert.NotEqual(t, "workflow/state_Space/Bugs", r.Holder, "own workflow is not an incoming ref")
	}
}

func TestDBDeleteBatch(t *testing.T) {
	cmds := dbDeleteBatch("Space/Bugs", []string{"Space/Priority_Space/Bugs"}, false)
	assert.Len(t, cmds, 2)
	first := cmds[0].(map[string]any)["args"].(map[string]any)
	assert.Equal(t, "Space/Bugs", first["name"])
	assert.Equal(t, false, first["delete-entities?"])
	assert.Equal(t, true, first["delete-related-fields?"])
	second := cmds[1].(map[string]any)["args"].(map[string]any)
	assert.Equal(t, "Space/Priority_Space/Bugs", second["name"])
	assert.Equal(t, true, second["delete-entities?"], "enum values always go with the database")

	withEntities := dbDeleteBatch("Space/Bugs", nil, true)
	assert.Equal(t, true, withEntities[0].(map[string]any)["args"].(map[string]any)["delete-entities?"])
}

func TestRequireDatabase(t *testing.T) {
	s := refsFixture()
	assert.NoError(t, requireDatabase(s, "Space/Bugs"))
	assert.ErrorContains(t, requireDatabase(s, "Space/Bug"), `did you mean "Space/Bugs"`)
	assert.Error(t, requireDatabase(s, "Nope/Nothing"))
	assert.Error(t, requireDatabase(s, "Space/Priority_Space/Bugs"), "helper types are not databases")
}
