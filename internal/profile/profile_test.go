package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FIBERY_HOME", dir)

	p := &Profiles{Profiles: map[string]Profile{}}
	p.Set("Development/Dev Task", Profile{
		Keep:     []string{"Development/name", "workflow/state"},
		Docs:     []string{"Development/description"},
		Comments: "full",
		Edges:    Edges{Parent: []string{"Development/Dev epic"}, Quality: []string{"Development/bugs"}},
	})
	require.NoError(t, Save(p, ""))

	// File lands in the account dir.
	_, err := os.Stat(filepath.Join(dir, "context.yaml"))
	require.NoError(t, err)

	got, err := Load("")
	require.NoError(t, err)
	prof, ok := got.Get("Development/Dev Task")
	require.True(t, ok)
	assert.Equal(t, "full", prof.Comments)
	assert.Equal(t, []string{"Development/Dev epic"}, prof.Edges.Parent)
	assert.Equal(t, []string{"Development/description"}, prof.Docs)
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	t.Setenv("FIBERY_HOME", t.TempDir())
	p, err := Load("personal")
	require.NoError(t, err)
	_, ok := p.Get("anything")
	assert.False(t, ok)
}
