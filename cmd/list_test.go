package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCountJSONArray(t *testing.T) {
	assert.Equal(t, 3, countJSONArray(json.RawMessage(`[1,2,3]`)))
	assert.Equal(t, 0, countJSONArray(json.RawMessage(`[]`)))
	assert.Equal(t, 0, countJSONArray(json.RawMessage(`{"x":1}`)))
	assert.Equal(t, 0, countJSONArray(json.RawMessage(`not json`)))
}

func TestPaginationHint_AtLimit(t *testing.T) {
	var buf bytes.Buffer
	paginationHint(&buf, json.RawMessage(`[1,2,3]`), 3)
	assert.Contains(t, buf.String(), "limit reached")
	assert.Contains(t, buf.String(), "--limit 6")
}

func TestPaginationHint_BelowLimit(t *testing.T) {
	var buf bytes.Buffer
	paginationHint(&buf, json.RawMessage(`[1,2]`), 50)
	assert.Empty(t, buf.String())
}

func TestPaginationHint_Empty(t *testing.T) {
	var buf bytes.Buffer
	paginationHint(&buf, json.RawMessage(`[]`), 50)
	assert.Empty(t, buf.String())
}

func TestResolveSortField(t *testing.T) {
	cases := []struct {
		input string
		field string
		desc  bool
	}{
		{"created", "fibery/creation-date", false},
		{"-created", "fibery/creation-date", true},
		{"modified", "fibery/modification-date", false},
		{"-modified", "fibery/modification-date", true},
		{"Space/Name", "Space/Name", false},
		{"-Space/Name", "Space/Name", true},
	}
	for _, c := range cases {
		field, desc := resolveSortField(c.input)
		assert.Equal(t, c.field, field, "field for %q", c.input)
		assert.Equal(t, c.desc, desc, "desc for %q", c.input)
	}
}
