package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/iamnikolie/fibery-cli/internal/profile"
)

func TestDraftYAML(t *testing.T) {
	prof := profile.Profile{
		Keep:     []string{"Development/name", "workflow/state"},
		Docs:     []string{"Development/description"},
		Comments: "full",
		Edges:    profile.Edges{Parent: []string{"Development/Dev epic"}},
	}
	out, err := draftYAML("Development/Dev Task", prof, []string{"Development/Memstore"})
	require.NoError(t, err)
	assert.Contains(t, out, "profiles:")
	assert.Contains(t, out, "Development/Dev Task")
	assert.Contains(t, out, "Development/Dev epic")
	assert.Contains(t, out, "# unclassified relation fields")
	assert.Contains(t, out, "Development/Memstore")
	// The draft must be valid YAML for the profiles map.
	var p profile.Profiles
	require.NoError(t, yamlUnmarshalForTest(out, &p))
	_, ok := p.Get("Development/Dev Task")
	assert.True(t, ok)
}

func yamlUnmarshalForTest(s string, v any) error {
	return yaml.Unmarshal([]byte(s), v)
}
