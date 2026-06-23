package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitFilter(t *testing.T) {
	cases := []struct {
		in               string
		field, op, value string
		ok               bool
	}{
		{"Development/Dev epic=2319", "Development/Dev epic", "=", "2319", true},
		{"workflow/state!=Done", "workflow/state", "!=", "Done", true},
		{"Space/Name~login", "Space/Name", "~", "login", true},
		{"Space/Field=a=b", "Space/Field", "=", "a=b", true}, // only first operator splits
		{"noOperatorHere", "", "", "", false},
		{"=novalue", "", "", "", false}, // empty field
	}
	for _, c := range cases {
		f, op, v, ok := splitFilter(c.in)
		assert.Equal(t, c.ok, ok, "ok for %q", c.in)
		if !c.ok {
			continue
		}
		assert.Equal(t, c.field, f, "field for %q", c.in)
		assert.Equal(t, c.op, op, "op for %q", c.in)
		assert.Equal(t, c.value, v, "value for %q", c.in)
	}
}

// filterTestSchema is a small schema with one of each filterable field kind.
var filterTestSchema = map[string]any{
	"fibery/types": []any{
		map[string]any{
			"fibery/name": "Development/Dev Task",
			"fibery/fields": []any{
				map[string]any{"fibery/name": "Development/Name", "fibery/type": "fibery/text"},
				map[string]any{"fibery/name": "Development/Dev epic", "fibery/type": "Development/Dev epic"},
				map[string]any{"fibery/name": "workflow/state", "fibery/type": "workflow/state_Development/Dev Task"},
				map[string]any{"fibery/name": "assignments/assignees", "fibery/type": "fibery/user"},
			},
		},
	},
}

func TestFieldFilterPath(t *testing.T) {
	db := "Development/Dev Task"
	assert.Equal(t, []any{"Development/Name"},
		fieldFilterPath(filterTestSchema, db, "Development/Name"))
	assert.Equal(t, []any{"Development/Dev epic", "fibery/public-id"},
		fieldFilterPath(filterTestSchema, db, "Development/Dev epic"))
	assert.Equal(t, []any{"workflow/state", "enum/name"},
		fieldFilterPath(filterTestSchema, db, "workflow/state"))
	assert.Equal(t, []any{"assignments/assignees", "user/email"},
		fieldFilterPath(filterTestSchema, db, "assignments/assignees"))
}

func TestBuildFilterClause_Single(t *testing.T) {
	db := "Development/Dev Task"
	clause, params, err := buildFilterClause(filterTestSchema, db, []string{"Development/Dev epic=2319"})
	assert.NoError(t, err)
	assert.Equal(t, []any{"=", []any{"Development/Dev epic", "fibery/public-id"}, "$flt0"}, clause)
	assert.Equal(t, map[string]any{"$flt0": "2319"}, params)
}

func TestBuildFilterClause_MultipleAnded(t *testing.T) {
	db := "Development/Dev Task"
	clause, params, err := buildFilterClause(filterTestSchema, db,
		[]string{"Development/Dev epic=2319", "workflow/state!=Done"})
	assert.NoError(t, err)
	expected := []any{
		"q/and",
		[]any{"=", []any{"Development/Dev epic", "fibery/public-id"}, "$flt0"},
		[]any{"!=", []any{"workflow/state", "enum/name"}, "$flt1"},
	}
	assert.Equal(t, expected, clause)
	assert.Equal(t, map[string]any{"$flt0": "2319", "$flt1": "Done"}, params)
}

func TestBuildFilterClause_Contains(t *testing.T) {
	db := "Development/Dev Task"
	clause, _, err := buildFilterClause(filterTestSchema, db, []string{"Development/Name~login"})
	assert.NoError(t, err)
	assert.Equal(t,
		[]any{"q/contains-ignoring-case?", []any{"Development/Name"}, "$flt0"}, clause)
}

func TestBuildFilterClause_CaseCorrectsFieldName(t *testing.T) {
	db := "Development/Dev Task"
	clause, _, err := buildFilterClause(filterTestSchema, db, []string{"development/name=x"})
	assert.NoError(t, err)
	// Field name is canonicalized back to "Development/Name" from the schema.
	assert.Equal(t, []any{"=", []any{"Development/Name"}, "$flt0"}, clause)
}

func TestBuildFilterClause_InvalidSyntax(t *testing.T) {
	_, _, err := buildFilterClause(filterTestSchema, "Development/Dev Task", []string{"noOperator"})
	assert.Error(t, err)
}
