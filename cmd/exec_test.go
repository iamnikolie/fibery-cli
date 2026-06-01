package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsScalarResult(t *testing.T) {
	cases := []struct {
		raw    string
		scalar bool
	}{
		{`"ok"`, true},
		{`42`, true},
		{`true`, true},
		{`{"x":1}`, false},
		{`[1,2]`, false},
		{`null`, false},
	}
	for _, c := range cases {
		got := isScalarResult(json.RawMessage(c.raw))
		assert.Equal(t, c.scalar, got, c.raw)
	}
}

func TestIsDestructiveCommand(t *testing.T) {
	assert.True(t, isDestructiveCommand("fibery.entity/delete"))
	assert.True(t, isDestructiveCommand("fibery.entity/remove-collection-items"))
	assert.True(t, isDestructiveCommand("schema/drop-database"))
	assert.True(t, isDestructiveCommand("Fibery.Entity/DELETE"))
	assert.False(t, isDestructiveCommand("fibery.entity/create"))
	assert.False(t, isDestructiveCommand("fibery.entity/update"))
	assert.False(t, isDestructiveCommand("fibery.entity/query"))
}
