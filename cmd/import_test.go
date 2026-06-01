package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyImportRow(t *testing.T) {
	row := map[string]json.RawMessage{
		"Space/Name":     json.RawMessage(`"First item"`),
		"Space/Priority": json.RawMessage(`"High"`),
		"Space/Tags":     json.RawMessage(`["Backend","API"]`),
		"Space/Score":    json.RawMessage(`42`),
	}

	scalars, arrays, passthrough := classifyImportRow(row, nil)

	// Array field → arrays bucket
	assert.Contains(t, arrays, "Space/Tags")
	assert.Len(t, arrays["Space/Tags"], 2)
	assert.NotContains(t, scalars, "Space/Tags")
	assert.NotContains(t, passthrough, "Space/Tags")

	// String fields → scalars
	assert.Equal(t, "First item", scalars["Space/Name"])
	assert.Equal(t, "High", scalars["Space/Priority"])

	// Number → passthrough (as float64)
	assert.Equal(t, float64(42), passthrough["Space/Score"])
}

func TestClassifyImportRow_CanonicalizesFieldNames(t *testing.T) {
	// Lowercased user input should land under the canonical schema name.
	row := map[string]json.RawMessage{
		"space/name": json.RawMessage(`"hello"`),
	}
	canon := func(k string) string {
		if k == "space/name" {
			return "Space/Name"
		}
		return k
	}
	scalars, _, _ := classifyImportRow(row, canon)
	assert.Equal(t, "hello", scalars["Space/Name"])
	assert.NotContains(t, scalars, "space/name")
}
