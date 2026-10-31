package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileLinks(t *testing.T) {
	md := "text ![a](https://acme.fibery.io/api/files/0123456789abcdef0123?w=1) and ![b](/api/files/0123456789abcdef0123?w=1) " +
		"again ![a](https://acme.fibery.io/api/files/0123456789abcdef0123?w=1) [x](https://example.com/y)"
	got := fileLinks(md)
	assert.Len(t, got, 2)
	assert.Equal(t, "0123456789abcdef0123", got[0][1])
	assert.Equal(t, "/api/files/0123456789abcdef0123?w=1", got[1][0])
}

func TestExportFileName(t *testing.T) {
	assert.Equal(t, "42-Fix-login-bug", exportFileName("42", "Fix login bug!"))
	assert.Equal(t, "7", exportFileName("7", "///"))
	assert.LessOrEqual(t, len(exportFileName("1", string(make([]byte, 0))+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")), 62)
}

func TestFormatValue(t *testing.T) {
	assert.Equal(t, "", formatValue(nil))
	assert.Equal(t, "true", formatValue(true))
	assert.Equal(t, "Epic A (#3)", formatValue(map[string]any{"Name": "Epic A", "Public ID": "3"}))
	assert.Equal(t, "#9", formatValue(map[string]any{"Public ID": "9"}))
	assert.Equal(t, "A, B (#2)", formatValue([]any{map[string]any{"Name": "A"}, map[string]any{"Name": "B", "Public ID": "2"}}))
	assert.Equal(t, "Done", formatValue(map[string]any{"enum/name": "Done"}))
}
