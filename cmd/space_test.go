package cmd

import (
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolderDeleteOrder(t *testing.T) {
	folders := []client.FolderRecord{
		{ID: "old", Name: "OLD", PublicID: "9"},
		{ID: "a", Name: "A", PublicID: "4", ParentID: "old"},
		{ID: "b", Name: "B", PublicID: "5", ParentID: "old"},
		{ID: "c", Name: "C", PublicID: "6"},
	}
	views := []client.ViewRecord{{ID: "v", FolderID: "c"}}

	_, err := folderDeleteOrder(folders[:1], folders, views)
	assert.ErrorContains(t, err, `holds folder`)

	got, err := folderDeleteOrder(folders[:3], folders, views)
	require.NoError(t, err)
	assert.Equal(t, "OLD", got[2].Name, "parent goes last")

	_, err = folderDeleteOrder(folders[3:], folders, views)
	assert.ErrorContains(t, err, "holds 1 view")
}

func TestSummarizeSpaces(t *testing.T) {
	apps := map[string]string{"s1": "Dev", "s2": "Wiki"}
	views := []client.ViewRecord{
		{ContainerAppID: "s2", ContainerType: "app", Type: "document"},
		{ContainerAppID: "s1", ContainerType: "app", Type: "board"},
		{ContainerAppID: "s1", ContainerType: "object", Type: "list"},
		{ContainerAppID: "sys", ContainerType: "app", Type: "list"},
	}
	folders := []client.FolderRecord{{ContainerAppID: "s1"}}
	got := summarizeSpaces(apps, []string{"Dev/Work", "Other/X"}, views, folders)
	require.Len(t, got, 2)
	assert.Equal(t, spaceSummary{Name: "Dev", ID: "s1", Databases: []string{"Dev/Work"}, Views: 1, Folders: 1}, got[0])
	assert.Equal(t, 1, got[1].Documents)
}
