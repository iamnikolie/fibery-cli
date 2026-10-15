package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamnikolie/fibery-cli/internal/profile"
)

func TestRenderEdgesCollapsesEmpty(t *testing.T) {
	meta := map[string]edgeAlias{
		"e1": {Role: "parent", Label: "Dev epic", Multi: false, TargetDB: "Development/Dev epic"},
		"e2": {Role: "quality", Label: "bugs", Multi: true},
		"e3": {Role: "blockers", Label: "Blocked By", Multi: true},
		"e4": {Role: "code", Label: "Merge Requests", Multi: true},
	}
	row := map[string]any{
		"e1_Name":  "Acme MCP",
		"e1_PID":   "2319",
		"e1_State": "Testing",
		"e2":       []any{map[string]any{"PID": "99", "Name": "bug x", "State": "Open", "Priority": "High"}},
		"e3":       []any{}, // empty
		"e4":       []any{}, // empty
	}
	var buf bytes.Buffer
	renderEdges(&buf, row, meta)
	out := buf.String()

	assert.Contains(t, out, "↑ Dev epic  2319  Acme MCP  [Testing]")
	assert.Contains(t, out, "### bugs (1)")
	assert.Contains(t, out, "| 99 | bug x | Open | High |")
	// Empty to-many edges collapse into one line; no per-edge "_none_" headers.
	assert.Contains(t, out, "_empty: Blocked By, Merge Requests_")
	assert.NotContains(t, out, "### Blocked By")
	assert.NotContains(t, out, "_none_")
}

func TestRenderEdgeTableDropsEmptyColumns(t *testing.T) {
	// MR-like rows: state present, priority absent → priority column omitted.
	var buf bytes.Buffer
	renderEdgeTable(&buf, "Merge Requests", []any{
		map[string]any{"PID": "29014", "Name": "[DT-5642] copy segments", "State": "merged", "Priority": ""},
	})
	out := buf.String()
	assert.Contains(t, out, "| id | name | state |")
	assert.NotContains(t, out, "priority")
	assert.Contains(t, out, "| 29014 | [DT-5642] copy segments | merged |")

	// Dev-Task-like rows: both present → full 4 columns.
	buf.Reset()
	renderEdgeTable(&buf, "Dev Tasks", []any{
		map[string]any{"PID": "5612", "Name": "get_programs", "State": "Dev Done", "Priority": "High"},
	})
	out = buf.String()
	assert.Contains(t, out, "| id | name | state | priority |")
	assert.Contains(t, out, "| 5612 | get_programs | Dev Done | High |")
}

func TestSelectFieldName(t *testing.T) {
	assert.Equal(t, "workflow/state", selectFieldName([]any{"workflow/state", "enum/name"}))
	assert.Equal(t, "assignments/assignees", selectFieldName(map[string]any{"q/from": "assignments/assignees"}))
	assert.Equal(t, "", selectFieldName(42))
}

func TestFilterSelectToProfile(t *testing.T) {
	full := map[string]any{
		"fibery/id":        []any{"fibery/id"},
		"Public ID":        []any{"fibery/public-id"},
		"Name":             []any{"Development/name"},
		"State":            []any{"workflow/state", "enum/name"},
		"Priority":         []any{"Development/Priority", "enum/name"},
		"Effort":           []any{"Development/effort"},
		"_doc_Description": []any{"Development/description", "Collaboration~Documents/secret"},
		"_doc_KT":          []any{"Development/Knowledge transfer", "Collaboration~Documents/secret"},
	}
	docKeys := []string{"_doc_Description", "_doc_KT"}
	prof := profile.Profile{
		Keep: []string{"Development/name", "workflow/state", "Development/Priority"},
		Docs: []string{"Development/description"},
	}
	sel, keptDocs := filterSelectToProfile(full, docKeys, prof)

	assert.Contains(t, sel, "fibery/id")
	assert.Contains(t, sel, "Public ID")
	assert.Contains(t, sel, "Name")
	assert.Contains(t, sel, "State")
	assert.Contains(t, sel, "Priority")
	assert.Contains(t, sel, "_doc_Description")
	assert.NotContains(t, sel, "Effort")  // not in keep
	assert.NotContains(t, sel, "_doc_KT") // not in docs
	assert.Equal(t, []string{"_doc_Description"}, keptDocs)
}

func TestContextBundleMarshal(t *testing.T) {
	b := contextBundle{
		Root:    json.RawMessage(`{"Name":"x"}`),
		Edges:   map[string][]json.RawMessage{"quality": {json.RawMessage(`{"PID":"1"}`)}},
		Parents: []json.RawMessage{json.RawMessage(`{"label":"Dev epic","pid":"2319"}`)},
	}
	out, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"root"`)
	assert.Contains(t, string(out), `"quality"`)
	assert.Contains(t, string(out), `2319`)
}
