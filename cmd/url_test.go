package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTitleToSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Fix login bug", "Fix-login-bug"},
		{"  spaced  out  ", "spaced-out"},
		{"Punctuation: a/b, c!", "Punctuation-a-b-c"},
		{"already-hyphenated", "already-hyphenated"},
		{"", ""},
		{"!!!", ""},
		{"Tabs\tand\nnewlines", "Tabs-and-newlines"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, titleToSlug(c.in), "slug for %q", c.in)
	}
}

func TestBuildEntityURL(t *testing.T) {
	base := "https://acme.fibery.io"

	// Space + database both have their spaces underscored; title becomes the slug.
	assert.Equal(t,
		"https://acme.fibery.io/Development/Dev_Task/Fix-login-bug-5651",
		buildEntityURL(base, "Development/Dev Task", "5651", "Fix login bug"))

	// Empty name falls back to the database name as the slug.
	assert.Equal(t,
		"https://acme.fibery.io/Development/Dev_Task/Dev-Task-42",
		buildEntityURL(base, "Development/Dev Task", "42", ""))

	// Trailing slash on the base URL is trimmed.
	assert.Equal(t,
		"https://acme.fibery.io/Support_platform/Support_ticket/X-100",
		buildEntityURL(base+"/", "Support platform/Support ticket", "100", "X"))
}
