package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
