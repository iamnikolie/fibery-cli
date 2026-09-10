package cmd

import (
	"fmt"
	"strings"
)

// filterOperators maps a user-facing operator token to its FQL function name.
// Order matters for splitFilter: a token that is a prefix of another (none here)
// would need the longer one first; "!=" already sorts before "=" by start index.
var filterOperators = []struct {
	token string
	fql   string
}{
	{"!=", "!="},
	{"~", "q/contains-ignoring-case?"},
	{"=", "="},
}

// splitFilter splits a "field<op>value" string into its three parts. It picks the
// operator with the earliest start index; on a tie it prefers the longer token so
// "!=" wins over "=". Returns ok=false when no operator is present or the field is
// empty.
func splitFilter(s string) (field, opToken, value string, ok bool) {
	bestIdx := -1
	best := ""
	for _, op := range filterOperators {
		i := strings.Index(s, op.token)
		if i < 0 {
			continue
		}
		if bestIdx == -1 || i < bestIdx || (i == bestIdx && len(op.token) > len(best)) {
			bestIdx, best = i, op.token
		}
	}
	if bestIdx <= 0 {
		return "", "", "", false
	}
	return s[:bestIdx], best, s[bestIdx+len(best):], true
}

// fieldFilterPath returns the FQL field navigation path used to compare a filter
// value against the given field, derived from its schema type:
//   - fibery/user      → [field, "user/email"]   (match by email)
//   - enum / workflow  → [field, "enum/name"]     (match by value name)
//   - primitive        → [field]                  (direct value)
//   - relation (entity)→ [field, "fibery/public-id"] (match by public id)
func fieldFilterPath(schema map[string]any, db, field string) []any {
	ftype := findFieldType(schema, db, field)
	switch {
	case ftype == "fibery/user":
		return []any{field, "user/email"}
	case isEnumLikeType(ftype):
		return []any{field, "enum/name"}
	case primitiveFieldTypes[ftype]:
		return []any{field}
	case strings.Contains(ftype, "/"):
		// Relation to another database — match by the related entity's public id.
		return []any{field, "fibery/public-id"}
	default:
		return []any{field}
	}
}

// buildFilterClause turns simple "field=value" filters into an FQL where-clause and
// its param map. Multiple filters are ANDed. Supported operators: = != ~ (contains).
// Field names are case-corrected and validated against the schema. Params are named
// $flt0, $flt1, … so they don't collide with user-supplied --params.
func buildFilterClause(schema map[string]any, db string, filters []string) (any, map[string]any, error) {
	clauses := make([]any, 0, len(filters))
	params := map[string]any{}
	for i, f := range filters {
		field, opToken, value, ok := splitFilter(f)
		if !ok {
			return nil, nil, fmt.Errorf("invalid --filter %q: expected field=value (operators: =, !=, ~)", f)
		}
		field = resolveFieldName(schema, db, strings.TrimSpace(field))
		if err := validateField(schema, db, field); err != nil {
			return nil, nil, err
		}
		fql := ""
		for _, op := range filterOperators {
			if op.token == opToken {
				fql = op.fql
				break
			}
		}
		p := fmt.Sprintf("$flt%d", i)
		params[p] = value
		clauses = append(clauses, []any{fql, fieldFilterPath(schema, db, field), p})
	}
	if len(clauses) == 1 {
		return clauses[0], params, nil
	}
	out := make([]any, 0, len(clauses)+1)
	out = append(out, "q/and")
	out = append(out, clauses...)
	return out, params, nil
}
