package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStatesDefaultsTypesByPosition(t *testing.T) {
	got, err := parseStates([]string{"Backlog", "In Progress", "Review", "Done"})
	require.NoError(t, err)
	assert.Equal(t, []stateSpec{
		{"Backlog", "Not started"}, {"In Progress", "Started"}, {"Review", "Started"}, {"Done", "Finished"},
	}, got)
}

func TestParseStatesExplicitTypes(t *testing.T) {
	got, err := parseStates([]string{"Backlog", "Done=finished", "Rejected = Finished"})
	require.NoError(t, err)
	assert.Equal(t, []stateSpec{{"Backlog", "Not started"}, {"Done", "Finished"}, {"Rejected", "Finished"}}, got)
}

func TestParseStatesErrors(t *testing.T) {
	for _, in := range [][]string{
		{"Doing=started", "Done"}, // first must be not-started
		{"A", "a"},                // duplicate
		{"A", "B=paused"},         // unknown type
		{"=started"},              // no name
	} {
		_, err := parseStates(in)
		assert.Error(t, err, "%v", in)
	}
	got, err := parseStates(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDBCreateBatchFull(t *testing.T) {
	b, err := dbCreateBatch(dbCreateSpec{DB: "Space/Work", Workflow: true, Comments: true, Files: true, Description: true})
	require.NoError(t, err)
	require.Len(t, b, 2)

	create := b[0].(map[string]any)
	assert.Equal(t, "schema.type/create", create["command"])
	args := create["args"].(map[string]any)
	assert.Equal(t, "Space/Work", args["fibery/name"])
	var names []string
	for _, f := range args["fibery/fields"].([]any) {
		fm := f.(map[string]any)
		names = append(names, fm["fibery/name"].(string))
		if fm["fibery/name"] == "Space/Name" {
			meta := fm["fibery/meta"].(map[string]any)
			assert.Equal(t, true, meta["ui/title?"])
			_, has := meta["fibery/secured?"]
			assert.False(t, has, "title field must not set fibery/secured? (refused by the API)")
		}
	}
	assert.Equal(t, []string{"Space/Name", "fibery/id", "fibery/public-id", "fibery/creation-date",
		"fibery/modification-date", "Space/Description", "Space/Files"}, names)

	mix := b[1].(map[string]any)["args"].(map[string]any)["types"].(map[string]any)["Space/Work"]
	assert.Equal(t, []any{"fibery/rank-mixin", "Collaboration~Documents/ReferencesMixin", "workflow/workflow", "comments/comments-mixin"}, mix)
}

func TestDBCreateBatchMinimal(t *testing.T) {
	b, err := dbCreateBatch(dbCreateSpec{DB: "Space/Log"})
	require.NoError(t, err)
	assert.Len(t, b[0].(map[string]any)["args"].(map[string]any)["fibery/fields"], 5)
	mix := b[1].(map[string]any)["args"].(map[string]any)["types"].(map[string]any)["Space/Log"]
	assert.Equal(t, []any{"fibery/rank-mixin", "Collaboration~Documents/ReferencesMixin"}, mix)
}

func TestDBCreateBatchRejectsBadNames(t *testing.T) {
	for _, db := range []string{"Work", "Space/", "/Work", "Space/A/B", "Space/Work_x"} {
		_, err := dbCreateBatch(dbCreateSpec{DB: db})
		assert.Error(t, err, db)
	}
}

func TestStateReplaceCommands(t *testing.T) {
	n := 0
	newID := func() string { n++; return fmt.Sprintf("id-%d", n) }
	c := stateReplaceCommands("Space/Work", []stateSpec{{"Backlog", "Not started"}, {"Done", "Finished"}}, []string{"old-1", "old-2"}, newID)
	assert.Equal(t, "fibery.command/batch", c.Command)
	cmds := c.Args.(map[string]any)["commands"].([]any)
	require.Len(t, cmds, 5)

	first := cmds[0].(map[string]any)["args"].(map[string]any)
	assert.Equal(t, "workflow/state_Space/Work", first["type"])
	e := first["entity"].(map[string]any)
	assert.Equal(t, "Backlog", e["enum/name"])
	assert.Equal(t, "Not started", e["workflow/Type"])
	assert.Equal(t, false, e["workflow/Final"])
	assert.Equal(t, true, cmds[1].(map[string]any)["args"].(map[string]any)["entity"].(map[string]any)["workflow/Final"])

	def := cmds[2].(map[string]any)["args"].(map[string]any)["commands"].([]any)[0].(map[string]any)["args"].(map[string]any)
	assert.Equal(t, "workflow/state", def["name"])
	assert.Equal(t, map[string]any{"fibery/id": "id-1"}, def["value"])

	assert.Equal(t, "fibery.entity/delete", cmds[3].(map[string]any)["command"])
	assert.Equal(t, map[string]any{"fibery/id": "old-2"}, cmds[4].(map[string]any)["args"].(map[string]any)["entity"])
}

func TestParseStatesByName(t *testing.T) {
	got, err := parseStates([]string{"Backlog", "To Do", "In Progress", "Done", "Rejected"})
	require.NoError(t, err)
	assert.Equal(t, []stateSpec{{"Backlog", "Not started"}, {"To Do", "Not started"}, {"In Progress", "Started"},
		{"Done", "Finished"}, {"Rejected", "Finished"}}, got)
}
