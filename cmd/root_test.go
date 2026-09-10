package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSchemaAgeWarning_Fresh(t *testing.T) {
	// Within threshold → no warning.
	got := schemaAgeWarning(time.Now().Add(-1*time.Hour), 7*24*time.Hour)
	assert.Empty(t, got)
}

func TestSchemaAgeWarning_AtBoundary(t *testing.T) {
	// Exactly at threshold → still no warning (strict >, not >=).
	got := schemaAgeWarning(time.Now().Add(-7*24*time.Hour+time.Second), 7*24*time.Hour)
	assert.Empty(t, got)
}

func TestSchemaAgeWarning_Stale(t *testing.T) {
	got := schemaAgeWarning(time.Now().Add(-10*24*time.Hour), 7*24*time.Hour)
	assert.Contains(t, got, "10 days old")
	assert.Contains(t, got, "fibery schema sync")
}

func TestAuthExempt(t *testing.T) {
	// Reachable before the user has any credentials.
	for _, name := range []string{
		"fibery", "init", "version", "skill", "help", "completion",
		"bash", "zsh", "fish", "powershell",
	} {
		assert.True(t, authExempt(name), "%s must run without a token", name)
	}

	// Everything that talks to the API must not be exempt.
	for _, name := range []string{"get", "list", "query", "create", "update", "comment", "doc", "files"} {
		assert.False(t, authExempt(name), "%s must require a token", name)
	}
}
