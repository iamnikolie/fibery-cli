package cmd

import (
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/stretchr/testify/assert"
)

func TestSplitFolderPath(t *testing.T) {
	assert.Equal(t, []string{"A", "B C", "D"}, splitFolderPath(" A / B C//D/ "))
	assert.Nil(t, splitFolderPath(""))
}

func TestWalkFolderPath(t *testing.T) {
	folders := []client.FolderRecord{
		{ID: "a", Name: "Archive", ContainerAppID: "s1"},
		{ID: "b", Name: "LINC", ContainerAppID: "s1", ParentID: "a"},
		{ID: "x", Name: "LINC", ContainerAppID: "s2"},
	}
	p, i := walkFolderPath(folders, "s1", []string{"Archive", "LINC", "Web3"})
	assert.Equal(t, "b", p)
	assert.Equal(t, 2, i)
	p, i = walkFolderPath(folders, "s1", []string{"LINC"})
	assert.Equal(t, "", p, "a root LINC in another space does not count")
	assert.Equal(t, 0, i)
	_, i = walkFolderPath(folders, "s1", []string{"Archive", "LINC"})
	assert.Equal(t, 2, i)
}
