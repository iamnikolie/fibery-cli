package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/langgerone/fibery-cli/internal/client"
)

func TestLooksLikeURL(t *testing.T) {
	assert.True(t, looksLikeURL("https://acme.fibery.io/Development/Doc-280"))
	assert.True(t, looksLikeURL("http://x.fibery.io/A/B-1"))
	assert.False(t, looksLikeURL("280"))
	assert.False(t, looksLikeURL("DT-42"))
	assert.False(t, looksLikeURL("550e8400-e29b-41d4-a716-446655440000"))
	assert.False(t, looksLikeURL("abc123secret"))
}

func TestURLTitleSlug(t *testing.T) {
	assert.Equal(t, "Quarterly-Planning-Notes", urlTitleSlug("https://acme.fibery.io/Development/Quarterly-Planning-Notes-2746"))
	assert.Equal(t, "Log-queries", urlTitleSlug("https://x.fibery.io/Development/Log-queries-1010"))
	assert.Equal(t, "Some-bug", urlTitleSlug("https://x.fibery.io/Dev/bug/Some-bug-3245"))
	assert.Equal(t, "NoTrailingNumber", urlTitleSlug("https://x.fibery.io/Space/NoTrailingNumber"))
}

func TestSlugifyTitle(t *testing.T) {
	assert.Equal(t, "Move-client-to-EU", slugifyTitle("Move client to EU"))
	assert.Equal(t, "Predrill-24-06-2025", slugifyTitle("Predrill 24/06/2025"))
	assert.Equal(t, "Quarterly-Report-v2", slugifyTitle("Quarterly Report — v2"))
	assert.Equal(t, "Trailing", slugifyTitle("  Trailing  "))
}

func TestFilterDocumentViews(t *testing.T) {
	views := []client.ViewRecord{
		{Name: "Dev policy", PublicID: "280", Type: "document", DocumentSecret: "s1", ContainerAppID: "dev"},
		{Name: "Sprint board", PublicID: "2", Type: "board", ContainerAppID: "dev"},
		{Name: "RM notes", PublicID: "574", Type: "document", DocumentSecret: "s2", ContainerAppID: "rm"},
	}
	appNames := map[string]string{"dev": "Development", "rm": "Releases"}

	// No space filter → all documents (boards excluded), space resolved.
	all := filterDocumentViews(views, appNames, "")
	assert.Len(t, all, 2)
	assert.Equal(t, "Dev policy", all[0].Name)
	assert.Equal(t, "Development", all[0].Space)
	assert.Equal(t, "Releases", all[1].Space)

	// Space filter (case-insensitive) → only matching space.
	dev := filterDocumentViews(views, appNames, "development")
	assert.Len(t, dev, 1)
	assert.Equal(t, "280", dev[0].PublicID)
}
