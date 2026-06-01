package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeFQLParams_RewritesQuestionMarkKeys(t *testing.T) {
	// Native Fibery FQL uses ?var, the CLI uses $var. Accept both so the user
	// doesn't get a server-side "Dangling meta character '?'" crash.
	in := map[string]any{"?id": "42", "?name": "foo"}
	got := normalizeFQLParams(in)
	assert.Equal(t, map[string]any{"$id": "42", "$name": "foo"}, got)
}

func TestNormalizeFQLParams_LeavesDollarKeysAlone(t *testing.T) {
	in := map[string]any{"$id": "42"}
	got := normalizeFQLParams(in)
	assert.Equal(t, map[string]any{"$id": "42"}, got)
}

func TestNormalizeFQLParams_NilSafe(t *testing.T) {
	assert.Nil(t, normalizeFQLParams(nil))
}

func TestNormalizeFQLQuery_RewritesParamReferences(t *testing.T) {
	// "?id" anywhere in the query (as a bare string referring to a param)
	// should become "$id" before hitting the API.
	q := []any{"=", []any{"fibery/public-id"}, "?id"}
	got := normalizeFQLQuery(q)
	assert.Equal(t, []any{"=", []any{"fibery/public-id"}, "$id"}, got)
}

func TestNormalizeFQLQuery_WalksNestedObjects(t *testing.T) {
	q := map[string]any{
		"q/from":   "Space/DB",
		"q/where":  []any{"=", []any{"fibery/public-id"}, "?id"},
		"q/select": map[string]any{"ID": []any{"fibery/public-id"}},
		"q/limit":  1,
	}
	got := normalizeFQLQuery(q).(map[string]any)
	where := got["q/where"].([]any)
	assert.Equal(t, "$id", where[2])
}

func TestNormalizeFQLQuery_LeavesNonReferenceStringsAlone(t *testing.T) {
	// A search-text containing a literal ? must not be munged.
	q := []any{"q/contains-ignoring-case?", []any{"Space/Name"}, "what is this?"}
	got := normalizeFQLQuery(q).([]any)
	assert.Equal(t, "what is this?", got[2])
}

func TestNormalizeFQLQuery_LeavesQuestionMarkOperatorsAlone(t *testing.T) {
	// "q/contains-ignoring-case?" ends with ? but is an operator name, not a
	// param reference (no leading ?). Must pass through unchanged.
	q := []any{"q/contains-ignoring-case?", []any{"Space/Name"}, "$q"}
	got := normalizeFQLQuery(q).([]any)
	assert.Equal(t, "q/contains-ignoring-case?", got[0])
}
