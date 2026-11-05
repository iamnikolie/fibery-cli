package cmd

import (
	"encoding/json"
	"testing"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolderPathsAndUnderFolder(t *testing.T) {
	paths := folderPaths([]client.FolderRecord{
		{ID: "a", Name: "Archive"},
		{ID: "b", Name: "Old", ParentID: "a"},
		{ID: "c", Name: "Deep", ParentID: "b"},
		{ID: "d", Name: "Archived"},
	})
	assert.Equal(t, "Archive/Old/Deep", paths["c"])
	assert.True(t, underFolder(paths["c"], "Archive/Old"))
	assert.True(t, underFolder(paths["b"], " Archive / Old "))
	assert.False(t, underFolder(paths["d"], "Archive"), "a name prefix is not a parent folder")
}

func TestDropNodesAndPrefixDoc(t *testing.T) {
	var doc any
	require.NoError(t, json.Unmarshal([]byte(`{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"hi"}]},
		{"type":"fiberyView","attrs":{"id":"v"}},
		{"type":"bullet_list","content":[{"type":"fiberyView"}]}]}`), &doc))
	assert.Equal(t, 2, dropNodes(doc, "fiberyView"))
	prefixDoc(doc, "Ann: ")
	b, _ := json.Marshal(doc)
	assert.JSONEq(t, `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Ann: "},{"type":"text","text":"hi"}]},
		{"type":"bullet_list","content":[]}]}`, string(b))

	var heading any
	require.NoError(t, json.Unmarshal([]byte(`{"type":"doc","content":[{"type":"heading"}]}`), &heading))
	prefixDoc(heading, "Ann: ")
	b, _ = json.Marshal(heading)
	assert.JSONEq(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Ann: "}]},{"type":"heading"}]}`, string(b))
}

func TestUserMapAndCommentAuthors(t *testing.T) {
	u := &userMap{ids: map[string]string{"s-me": "d-me"}, names: map[string]string{"s-ann": "Ann"}, fallback: "d-me"}
	assert.Equal(t, "d-me", u.to("s-me"))
	assert.Equal(t, "d-me", u.to("s-ann"))
	assert.Equal(t, "", u.lost("s-me"))
	assert.Equal(t, "Ann", u.lost("s-ann"))

	var c any
	require.NoError(t, json.Unmarshal([]byte(`{"author":{"id":"s-ann"},"body":{"doc":{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"fix"}]}]}}}`), &c))
	mapCommentAuthors(c, u)
	b, _ := json.Marshal(c)
	assert.JSONEq(t, `{"author":{"id":"d-me"},"body":{"doc":{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Ann: "},{"type":"text","text":"fix"}]}]}}}`, string(b))
}

func TestSpaceCreateCommands(t *testing.T) {
	cmds := spaceCreateCommands("Dev", "#000000", "folder")
	require.Len(t, cmds, 2)
	assert.Equal(t, "fibery.app/save", cmds[0].Command)
	app := cmds[0].Args.(map[string]any)["app"].(map[string]any)
	assert.Equal(t, "Dev", app["fibery/namespace"])
	assert.Equal(t, "folder", app["fibery/icon"])
	assert.Equal(t, "fibery.app/install", cmds[1].Command)
	assert.Error(t, validSpaceName("A/B"))
	assert.Error(t, validSpaceName(" "))
	assert.NoError(t, validSpaceName("Development"))
}
