package cache_test

import (
	"encoding/json"
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_RoundTrip(t *testing.T) {
	t.Setenv("FIBERY_HOME", t.TempDir())

	assert.False(t, cache.HasSchema(""))

	schema := map[string]any{"spaces": []any{"Dev", "Infra"}}
	require.NoError(t, cache.SaveSchema(schema, ""))

	assert.True(t, cache.HasSchema(""))

	loaded, err := cache.LoadSchema("")
	require.NoError(t, err)

	// Re-marshal both for comparison since json round-trip normalises types
	want, _ := json.Marshal(schema)
	got, _ := json.Marshal(loaded)
	assert.JSONEq(t, string(want), string(got))
}

func TestCache_LoadSchema_Missing(t *testing.T) {
	t.Setenv("FIBERY_HOME", t.TempDir())
	_, err := cache.LoadSchema("")
	require.Error(t, err)
}

func TestCache_SchemaModTime_AfterSave(t *testing.T) {
	t.Setenv("FIBERY_HOME", t.TempDir())
	require.NoError(t, cache.SaveSchema(map[string]any{"x": 1}, ""))
	mt, err := cache.SchemaModTime("")
	require.NoError(t, err)
	assert.WithinDuration(t, mt, mt, 0) // sanity: returns a non-zero time
	assert.False(t, mt.IsZero())
}

func TestCache_SchemaModTime_Missing(t *testing.T) {
	t.Setenv("FIBERY_HOME", t.TempDir())
	_, err := cache.SchemaModTime("")
	require.Error(t, err)
}
