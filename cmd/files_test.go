package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "report.pdf", "report.pdf"},
		{"strips directory", "a/b/c.txt", "c.txt"},
		{"strips backslash directory", `a\b\c.txt`, "c.txt"},
		{"path traversal", "../../etc/passwd", "passwd"},
		{"dotdot becomes file", "..", "file"},
		{"single dot becomes file", ".", "file"},
		{"empty becomes file", "", "file"},
		{"strips ascii control chars", "ab\x01\x02\x1fcd.xlsx", "abcd.xlsx"},
		{"strips zero-width and format chars", "a\u200b\u200c\ufeffb.txt", "ab.txt"},
		{"trims surrounding spaces", "  spaced name.txt  ", "spaced name.txt"},
		{"keeps leading dot file", ".env", ".env"},
		{"keeps pipes (legal on unix)", "a|b.csv", "a|b.csv"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, sanitizeFilename(c.in))
		})
	}
}

func TestDedupeName(t *testing.T) {
	used := map[string]bool{}
	assert.Equal(t, "a.txt", dedupeName("a.txt", used))
	assert.Equal(t, "a (1).txt", dedupeName("a.txt", used))
	assert.Equal(t, "a (2).txt", dedupeName("a.txt", used))
	// no extension
	assert.Equal(t, "notes", dedupeName("notes", used))
	assert.Equal(t, "notes (1)", dedupeName("notes", used))
	// a distinct name is untouched
	assert.Equal(t, "b.txt", dedupeName("b.txt", used))
}

func TestDiscoverFileFields(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{
				"fibery/name": "Test/DB",
				"fibery/fields": []any{
					map[string]any{"fibery/name": "Files/Files", "fibery/type": "fibery/file", "fibery/meta": map[string]any{"fibery/collection?": true}},
					map[string]any{"fibery/name": "Test/Attachments", "fibery/type": "fibery/file", "fibery/meta": map[string]any{"fibery/collection?": true}},
					map[string]any{"fibery/name": "avatar/avatars", "fibery/type": "fibery/file", "fibery/meta": map[string]any{"fibery/collection?": true}},
					map[string]any{"fibery/name": "Test/Name", "fibery/type": "fibery/text", "fibery/meta": map[string]any{}},
				},
			},
		},
	}
	// Returns file-type collection fields, sorted, excluding the avatar system field.
	got := discoverFileFields(schema, "Test/DB")
	assert.Equal(t, []string{"Files/Files", "Test/Attachments"}, got)

	// Unknown DB → empty.
	assert.Empty(t, discoverFileFields(schema, "Nope/Nope"))
}

func TestBuildFilesQuery(t *testing.T) {
	q := buildFilesQuery("Development/Dev Task", []string{"Files/Files"}, "uuid-123")

	query := q["query"].(map[string]any)
	assert.Equal(t, "Development/Dev Task", query["q/from"])
	assert.Equal(t, 1, query["q/limit"])
	assert.Equal(t, []any{"=", []any{"fibery/id"}, "$id"}, query["q/where"])
	assert.Equal(t, map[string]any{"$id": "uuid-123"}, q["params"])

	sel := query["q/select"].(map[string]any)
	sub := sel["Files/Files"].(map[string]any)
	assert.Equal(t, "Files/Files", sub["q/from"])
	assert.Equal(t, "q/no-limit", sub["q/limit"])
	assert.Equal(t, map[string]any{
		"secret":         []any{"fibery/secret"},
		"name":           []any{"fibery/name"},
		"content-type":   []any{"fibery/content-type"},
		"content-length": []any{"fibery/content-length"},
	}, sub["q/select"])
}
