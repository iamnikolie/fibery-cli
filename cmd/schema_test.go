package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchemaRequiredDetection(t *testing.T) {
	requiredField := map[string]any{
		"fibery/meta": map[string]any{"fibery/required?": true},
	}
	optionalField := map[string]any{
		"fibery/meta": map[string]any{},
	}
	missingMeta := map[string]any{}

	check := func(fm map[string]any) bool {
		meta, _ := fm["fibery/meta"].(map[string]any)
		return meta["fibery/required?"] == true
	}

	assert.True(t, check(requiredField))
	assert.False(t, check(optionalField))
	assert.False(t, check(missingMeta))
}
