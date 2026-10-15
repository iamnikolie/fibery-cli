package cmd

import (
	"fmt"
	"strings"
)

// fieldNames returns all field names defined for db in the cached schema.
func fieldNames(schema map[string]any, db string) []string {
	var out []string
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			if fm, ok := f.(map[string]any); ok {
				out = append(out, asStr(fm["fibery/name"]))
			}
		}
	}
	return out
}

// validateField returns a "did you mean" error when field is not in db's schema
// and a close match exists. It returns nil when the field exists, or when nothing
// is close (so an unknown-but-not-close field still passes through to the API,
// which reports its own candidate list — avoids false rejects on a stale cache).
func validateField(schema map[string]any, db, field string) error {
	if schemaHasField(schema, db, field) {
		return nil
	}
	if suggestion, ok := closestFieldName(field, fieldNames(schema, db)); ok {
		return fmt.Errorf("field %q not found in %q — did you mean %q? (or run: fibery schema sync)", field, db, suggestion)
	}
	return nil
}

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// localName returns the part of a field name after the last "/" (e.g.
// "Development/Name" → "Name"), so suggestions compare the meaningful suffix and
// aren't dominated by the shared space prefix.
func localName(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// closestFieldName returns the candidate whose local name is closest to the typed
// name and true when it is within the typo threshold (edit distance ≤ 3 on the
// local name, compared case-insensitively).
func closestFieldName(name string, candidates []string) (string, bool) {
	local := strings.ToLower(localName(name))
	best := ""
	bestDist := 1 << 30
	for _, c := range candidates {
		d := levenshtein(local, strings.ToLower(localName(c)))
		if d < bestDist {
			bestDist, best = d, c
		}
	}
	if best == "" || bestDist > 3 {
		return "", false
	}
	return best, true
}
