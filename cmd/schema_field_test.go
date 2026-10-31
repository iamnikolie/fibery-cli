package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seqID() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("id-%d", n) }
}

// subCommands unwraps the top-level fibery.command/batch.
func subCommands(t *testing.T, a any) []map[string]any {
	t.Helper()
	args := a.(map[string]any)
	var out []map[string]any
	for _, c := range args["commands"].([]any) {
		out = append(out, c.(map[string]any))
	}
	return out
}

func schemaCmds(t *testing.T, batch map[string]any) []map[string]any {
	t.Helper()
	require.Equal(t, "fibery.schema/batch", batch["command"])
	return subCommands(t, batch["args"].(map[string]any))
}

func TestQualifyField(t *testing.T) {
	assert.Equal(t, "Space/Project", qualifyField("Space/Tasks", "Project"))
	assert.Equal(t, "Other/Project", qualifyField("Space/Tasks", "Other/Project"))
}

func TestFieldAdd_Primitive(t *testing.T) {
	c, err := fieldAddCommands(fieldSpec{DB: "Space/Tasks", Name: "Space/Link", Kind: "url"}, seqID())
	require.NoError(t, err)
	assert.Equal(t, "fibery.command/batch", c.Command)
	top := subCommands(t, c.Args)
	require.Len(t, top, 1)
	sc := schemaCmds(t, top[0])
	args := sc[0]["args"].(map[string]any)
	assert.Equal(t, "fibery/text", args["fibery/type"])
	assert.Equal(t, "url", args["fibery/meta"].(map[string]any)["ui/type"])
}

func TestFieldAdd_SingleSelect(t *testing.T) {
	c, err := fieldAddCommands(fieldSpec{
		DB: "Space/Tasks", Name: "Space/Project", Kind: "single-select",
		Options: []string{"Alpha", "Beta"}, Default: "beta", Required: true,
	}, seqID())
	require.NoError(t, err)
	top := subCommands(t, c.Args)
	require.Len(t, top, 4) // schema batch, 2 options, meta batch
	sc := schemaCmds(t, top[0])
	assert.Equal(t, "schema.enum/create", sc[0]["command"])
	assert.Equal(t, "Space/Project_Space/Tasks", sc[0]["args"].(map[string]any)["fibery/name"])
	assert.Equal(t, "Space/Project_Space/Tasks", sc[1]["args"].(map[string]any)["fibery/type"])

	opt := top[2]["args"].(map[string]any)["entity"].(map[string]any)
	assert.Equal(t, "Beta", opt["enum/name"])
	assert.Equal(t, "id-2", opt["fibery/id"])

	meta := schemaCmds(t, top[3])
	require.Len(t, meta, 2)
	assert.Equal(t, map[string]any{"fibery/id": "id-2"}, meta[0]["args"].(map[string]any)["value"])
	assert.Equal(t, "fibery/required?", meta[1]["args"].(map[string]any)["key"])
}

func TestFieldAdd_SingleSelectErrors(t *testing.T) {
	_, err := fieldAddCommands(fieldSpec{DB: "S/T", Name: "S/P", Kind: "single-select"}, seqID())
	assert.ErrorContains(t, err, "--option")
	_, err = fieldAddCommands(fieldSpec{DB: "S/T", Name: "S/P", Kind: "single-select", Options: []string{"A"}, Default: "Z"}, seqID())
	assert.ErrorContains(t, err, "not one of the options")
	_, err = fieldAddCommands(fieldSpec{DB: "S/T", Name: "S/P", Kind: "single-select", Options: []string{"A"}, Required: true}, seqID())
	assert.ErrorContains(t, err, "needs --default")
}

func TestFieldAdd_Relation(t *testing.T) {
	c, err := fieldAddCommands(fieldSpec{DB: "Space/Tasks", Name: "Space/Epic", Kind: "relation", To: "Plan/Epic"}, seqID())
	require.NoError(t, err)
	sc := schemaCmds(t, subCommands(t, c.Args)[0])
	require.Len(t, sc, 2)
	here := sc[0]["args"].(map[string]any)
	back := sc[1]["args"].(map[string]any)
	assert.Equal(t, "Plan/Epic", here["fibery/type"])
	assert.NotContains(t, here["fibery/meta"], "fibery/collection?")
	assert.Equal(t, "Plan/Epic", back["fibery/holder-type"])
	assert.Equal(t, "Plan/Tasks", back["fibery/name"], "back field defaults to the holder's name in the target space")
	assert.Equal(t, true, back["fibery/meta"].(map[string]any)["fibery/collection?"])
	assert.Equal(t, here["fibery/meta"].(map[string]any)["fibery/relation"], back["fibery/meta"].(map[string]any)["fibery/relation"])
}

func TestFieldAdd_Errors(t *testing.T) {
	_, err := fieldAddCommands(fieldSpec{DB: "Space/Tasks", Name: "Other/X", Kind: "text"}, seqID())
	assert.ErrorContains(t, err, "database's space")
	_, err = fieldAddCommands(fieldSpec{DB: "Space/Tasks", Name: "Space/X", Kind: "nope"}, seqID())
	assert.ErrorContains(t, err, "unknown --type")
	_, err = fieldAddCommands(fieldSpec{DB: "Space/Tasks", Name: "Space/X", Kind: "relation"}, seqID())
	assert.ErrorContains(t, err, "--to")
	_, err = fieldAddCommands(fieldSpec{DB: "Space/Tasks", Name: "Space/X", Kind: "text", Required: true}, seqID())
	assert.ErrorContains(t, err, "only supported for single-select")
}

func fieldDelFixture() map[string]any {
	rel := map[string]any{"fibery/name": "Space/Epic", "fibery/type": "Plan/Epic", "fibery/meta": map[string]any{"fibery/relation": "r1"}}
	back := map[string]any{"fibery/name": "Plan/Tasks", "fibery/type": "Space/Tasks", "fibery/meta": map[string]any{"fibery/relation": "r1", "fibery/collection?": true}}
	return map[string]any{"fibery/types": []any{
		sdbType("Space/Tasks", rel,
			map[string]any{"fibery/name": "Space/Project", "fibery/type": "Space/Project_Space/Tasks", "fibery/meta": map[string]any{"fibery/relation": "r2"}},
			sdbField("Space/Points", "fibery/int"),
			sdbField("workflow/state", "workflow/state_Space/Tasks"),
		),
		sdbType("Plan/Epic", back),
		sdbType("Space/Project_Space/Tasks", map[string]any{"fibery/name": "Space/entities", "fibery/type": "Space/Tasks", "fibery/meta": map[string]any{"fibery/relation": "r2", "fibery/collection?": true}}),
	}}
}

func TestFieldDeletePlan_Relation(t *testing.T) {
	p, err := buildFieldDeletePlan(fieldDelFixture(), "Space/Tasks", "Space/Epic", false)
	require.NoError(t, err)
	require.Len(t, p.Batch, 2, "both sides of a relation in one batch")
	assert.Equal(t, schemaRef{Holder: "Plan/Epic", Field: "Plan/Tasks", Target: "Space/Tasks"}, p.Fields[1])
	assert.Equal(t, "", p.EnumType)
}

func TestFieldDeletePlan_SingleSelect(t *testing.T) {
	p, err := buildFieldDeletePlan(fieldDelFixture(), "Space/Tasks", "Space/Project", true)
	require.NoError(t, err)
	assert.Equal(t, "Space/Project_Space/Tasks", p.EnumType)
	require.Len(t, p.Batch, 2, "enum back field goes with its type, not separately")
	assert.Len(t, p.Fields, 1)
	assert.Equal(t, true, p.Batch[0].(map[string]any)["args"].(map[string]any)["delete-values?"])
	assert.Equal(t, "schema.type/delete", p.Batch[1].(map[string]any)["command"])
}

func TestFieldDeletePlan_Refusals(t *testing.T) {
	_, err := buildFieldDeletePlan(fieldDelFixture(), "Space/Tasks", "workflow/state", false)
	assert.ErrorContains(t, err, "built-in/mixin")
	_, err = buildFieldDeletePlan(fieldDelFixture(), "Space/Tasks", "Space/Nope", false)
	assert.ErrorContains(t, err, "not found")
	p, err := buildFieldDeletePlan(fieldDelFixture(), "Space/Tasks", "Space/Points", false)
	require.NoError(t, err)
	assert.Len(t, p.Batch, 1)
}

func TestFieldAddUser(t *testing.T) {
	n := 0
	c, err := fieldAddCommands(fieldSpec{DB: "Space/Work", Name: "Space/Assignee", Kind: "user", Many: true},
		func() string { n++; return fmt.Sprintf("id-%d", n) })
	require.NoError(t, err)
	sch := c.Args.(map[string]any)["commands"].([]any)[0].(map[string]any)["args"].(map[string]any)["commands"].([]any)
	require.Len(t, sch, 2)
	here := sch[0].(map[string]any)["args"].(map[string]any)
	assert.Equal(t, "fibery/user", here["fibery/type"])
	assert.Equal(t, map[string]any{"fibery/relation": "id-1", "fibery/collection?": true}, here["fibery/meta"])
	back := sch[1].(map[string]any)["args"].(map[string]any)
	assert.Equal(t, "fibery/user", back["fibery/holder-type"])
	assert.Equal(t, "user/Work (Space - Assignee)", back["fibery/name"])
}

func TestFieldAddBoolDefaultsFalse(t *testing.T) {
	c, err := fieldAddCommands(fieldSpec{DB: "Space/Work", Name: "Space/Archive", Kind: "bool"}, func() string { return "x" })
	require.NoError(t, err)
	cmds := c.Args.(map[string]any)["commands"].([]any)
	require.Len(t, cmds, 2)
	meta := cmds[1].(map[string]any)["args"].(map[string]any)["commands"].([]any)
	assert.Equal(t, "fibery/default-value", meta[0].(map[string]any)["args"].(map[string]any)["key"])
	assert.Equal(t, false, meta[0].(map[string]any)["args"].(map[string]any)["value"])
	assert.Equal(t, true, meta[1].(map[string]any)["args"].(map[string]any)["value"])
}
