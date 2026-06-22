package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAggregateCountUnsupported(t *testing.T) {
	// The real error seen on permission-secured databases.
	assert.True(t, aggregateCountUnsupported("entity.error/query-with-permissions-expr-not-supported: Sub query aggregate expressions are not supported."))
	assert.True(t, aggregateCountUnsupported("Sub query aggregate expressions are not supported."))
	// Unrelated errors must NOT trigger the fallback.
	assert.False(t, aggregateCountUnsupported("database \"X/Y\" not found"))
	assert.False(t, aggregateCountUnsupported("HTTP 401"))
}
