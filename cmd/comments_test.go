package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderCommentsMarkdown(t *testing.T) {
	orig := fetchDocBody
	defer func() { fetchDocBody = orig }()
	fetchDocBody = func(_ context.Context, secret string) string {
		if secret == "" {
			return ""
		}
		return "body for " + secret
	}
	rows := []commentRow{
		{ID: "u1", PublicID: "101", Author: "Alice", Created: "2026-06-25T10:00:00Z", Secret: "s1"},
		{ID: "u2", PublicID: "", Author: "", Created: "2026-06-25T11:00:00Z", Secret: ""},
	}
	var buf bytes.Buffer
	renderComments(context.Background(), &buf, rows)
	out := buf.String()
	assert.Contains(t, out, "## Alice — 2026-06-25T10:00:00Z")
	assert.Contains(t, out, "_id: u1 · #101_")
	assert.Contains(t, out, "body for s1")
	assert.Contains(t, out, "## ? — 2026-06-25T11:00:00Z") // empty author → "?"
	assert.Contains(t, out, "_(empty)_")                   // no secret → empty body
}

func TestLatestN(t *testing.T) {
	items := []any{"a", "b", "c", "d"}

	// n within range returns the tail, oldest-first order preserved.
	assert.Equal(t, []any{"c", "d"}, latestN(items, 2))
	assert.Equal(t, []any{"d"}, latestN(items, 1))

	// n >= len or n <= 0 returns the slice unchanged.
	assert.Equal(t, items, latestN(items, 4))
	assert.Equal(t, items, latestN(items, 10))
	assert.Equal(t, items, latestN(items, 0))
	assert.Equal(t, items, latestN(items, -1))

	// empty input is safe.
	assert.Empty(t, latestN([]any{}, 3))
}

func TestParseSince_RFC3339(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("2026-06-20T00:00:00Z", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), got.UTC())
}

func TestParseSince_RelativeHours(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("24h", now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-24*time.Hour), got)
}

func TestParseSince_RelativeMinutes(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("30m", now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-30*time.Minute), got)
}

func TestParseSince_RelativeDays(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("7d", now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-7*24*time.Hour), got)
}

func TestParseSince_Invalid(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	_, err := parseSince("yesterday", now)
	require.Error(t, err)
	// Error must name an accepted format so the caller can recover.
	assert.Contains(t, err.Error(), "RFC3339")
	assert.Contains(t, err.Error(), "7d")
}

func TestParseSince_Empty(t *testing.T) {
	_, err := parseSince("", time.Now())
	require.Error(t, err)
}
