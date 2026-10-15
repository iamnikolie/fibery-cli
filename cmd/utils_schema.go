package cmd

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/iamnikolie/fibery-cli/internal/cache"
)

// validateDB returns a "did you mean" error when db is not a known database in
// the cached schema and a close match exists. It returns nil when db is known,
// when nothing is close (an unknown-but-not-close db still passes through to the
// API, which reports its own error — avoids false rejects on a stale cache), or
// when there is no usable cache to check against.
func validateDB(db string) error {
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return nil
	}
	dbs := listDatabases(schema)
	for _, d := range dbs {
		if d == db {
			return nil
		}
	}
	if suggestion, ok := closestDatabase(db, dbs); ok {
		return fmt.Errorf("database %q not found — did you mean %q? (run 'fibery schema sync' if it's new, or 'fibery search' to list databases)", db, suggestion)
	}
	return nil
}

// closestDatabase returns the candidate database name closest to db and true when
// it is within the typo threshold. It compares both the full "Space/Database"
// string and the local (post-slash) name case-insensitively, so it catches both
// ordinary typos and a dropped space prefix (e.g. "Dev Task" → "Development/Dev Task").
func closestDatabase(db string, candidates []string) (string, bool) {
	full := strings.ToLower(db)
	local := strings.ToLower(localName(db))
	best := ""
	bestDist := 1 << 30
	for _, c := range candidates {
		d := levenshtein(full, strings.ToLower(c))
		if dl := levenshtein(local, strings.ToLower(localName(c))); dl < d {
			d = dl
		}
		if d < bestDist {
			bestDist, best = d, c
		}
	}
	if best == "" || bestDist > 4 {
		return "", false
	}
	return best, true
}

// normalize removes all non-alphanumeric characters and lowercases a string.
func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// spaceDatabases returns all database names in the given Fibery space (non-platform, non-enum).
func spaceDatabases(space string) []string {
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return nil
	}
	spaceNorm := normalize(space)
	types, _ := schema["fibery/types"].([]any)
	var result []string
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name := asStr(tm["fibery/name"])
		if !strings.Contains(name, "/") {
			continue
		}
		// Skip platform types and enum helpers (name contains underscore after /)
		meta, _ := tm["fibery/meta"].(map[string]any)
		if meta["fibery/platform?"] == true || meta["fibery/primitive?"] == true {
			continue
		}
		if strings.Contains(strings.SplitN(name, "/", 2)[1], "_") {
			continue
		}
		parts := strings.SplitN(name, "/", 2)
		if normalize(parts[0]) == spaceNorm {
			result = append(result, name)
		}
	}
	return result
}

// findTitleField returns the field name marked as ui/title? in the schema for the given db.
func findTitleField(schema map[string]any, db string) string {
	return findFieldWhere(schema, db, func(meta map[string]any) bool {
		v, _ := meta["ui/title?"].(bool)
		return v
	})
}

// findDescriptionField returns the document field name (type Collaboration~Documents/Document) for the db.
func findDescriptionField(schema map[string]any, db string) (string, bool) {
	f := findFieldWhere(schema, db, func(_ map[string]any) bool { return false })
	_ = f
	// Search by type
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if asStr(fm["fibery/type"]) == "Collaboration~Documents/Document" {
				return asStr(fm["fibery/name"]), true
			}
		}
	}
	return "", false
}

// findFieldWhere returns the fibery/name of the first field matching predicate on its fibery/meta.
func findFieldWhere(schema map[string]any, db string, pred func(map[string]any) bool) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			meta, _ := fm["fibery/meta"].(map[string]any)
			if pred(meta) {
				return asStr(fm["fibery/name"])
			}
		}
	}
	return ""
}

// schemaHasField returns true if the given database has a field with that name.
func schemaHasField(schema map[string]any, db, fieldName string) bool {
	return findFieldByName(schema, db, fieldName) != ""
}

func findFieldByName(schema map[string]any, db, fieldName string) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if asStr(fm["fibery/name"]) == fieldName {
				return fieldName
			}
		}
	}
	return ""
}

func mustSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// skipFieldTypes are field types that add no user-visible value.
var skipFieldTypes = map[string]bool{
	"fibery/uuid": true, "fibery/rank": true, "fibery/file": true,
	"fibery/view": true, "fibery/json-value": true, "fibery/color": true,
	"fibery/emoji": true, "comments/comment": true,
	"Collaboration~Documents/Reference": true,
}

// primitiveFieldTypes can be selected directly with [fieldName].
var primitiveFieldTypes = map[string]bool{
	"fibery/text": true, "fibery/int": true, "fibery/decimal": true,
	"fibery/bool": true, "fibery/date-time": true, "fibery/date": true,
	"fibery/email": true, "fibery/date-range": true,
	"fibery/url": true, "fibery/date-time-range": true, "fibery/location": true,
}

// fieldAliasMap maps technical field names to human-readable aliases.
var fieldAliasMap = map[string]string{
	"fibery/public-id":      "ID",
	"fibery/creation-date":  "Created",
	"fibery/created-by":     "Created By",
	"workflow/state":        "State",
	"assignments/assignees": "Assignees",
}

func fieldAlias(name string) string {
	if a, ok := fieldAliasMap[name]; ok {
		return a
	}
	// Strip space prefix: "Development/Priority" → "Priority"
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

// buildFullSelect builds a complete q/select map for a database using its schema definition.
// Returns the select map and a slice of keys whose values are document secrets.
func buildFullSelect(schema map[string]any, db string) (sel map[string]any, docKeys []string) {
	// fibery/id and fibery/public-id are marked fibery/id?: true in the schema and
	// skipped by the field iteration below — inject them explicitly so the UUID is
	// always present in --json output, and the human-readable public ID is always
	// available for the LLM-friendly print.
	sel = map[string]any{
		"fibery/id": []any{"fibery/id"},
		"Public ID": []any{"fibery/public-id"},
	}

	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}

		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			fieldName := asStr(fm["fibery/name"])
			fieldType := asStr(fm["fibery/type"])
			meta, _ := fm["fibery/meta"].(map[string]any)

			// Skip internal IDs, rank, modification date
			if meta["fibery/id?"] == true || fieldName == "fibery/rank" ||
				fieldName == "fibery/modification-date" {
				continue
			}
			// Skip useless types
			if skipFieldTypes[fieldType] {
				continue
			}
			// Skip formula-derived collections (noisy duplicates)
			if meta["formula/formula?"] == true && meta["fibery/collection?"] == true {
				continue
			}

			// Title field gets canonical alias "Name"
			var alias string
			if meta["ui/title?"] == true {
				alias = "Name"
			} else {
				alias = fieldAlias(fieldName)
			}
			isCollection := meta["fibery/collection?"] == true

			switch {
			case isCollection:
				// Only include assignees collection; skip the rest
				if fieldName == "assignments/assignees" {
					sel[alias] = map[string]any{
						"q/from":   "assignments/assignees",
						"q/select": map[string]any{"Name": "user/name", "Email": "user/email"},
						"q/limit":  10,
					}
				}

			case primitiveFieldTypes[fieldType]:
				sel[alias] = []any{fieldName}

			case fieldType == "fibery/user":
				sel[alias] = []any{fieldName, "user/name"}

			case fieldType == "Collaboration~Documents/Document":
				key := "_doc_" + alias
				sel[key] = []any{fieldName, "Collaboration~Documents/secret"}
				docKeys = append(docKeys, key)

			case isEnumLikeType(fieldType):
				// Enum and workflow state types: navigate to enum/name
				sel[alias] = []any{fieldName, "enum/name"}

			default:
				// Single entity relation: get public-id
				sel[alias] = []any{fieldName, "fibery/public-id"}
			}
		}
		break
	}
	return sel, docKeys
}

// isEnumLikeType returns true for enum and workflow state types.
// These types have the pattern "Space/Type_Space/Database" or "workflow/state_*".
func isEnumLikeType(t string) bool {
	if !strings.Contains(t, "/") {
		return false
	}
	after := strings.SplitN(t, "/", 2)[1]
	return strings.Contains(after, "_")
}

// isCollectionField returns true if fieldName in db is marked as a collection (multi-value) field.
func isCollectionField(schema map[string]any, db, fieldName string) bool {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if asStr(fm["fibery/name"]) == fieldName {
				meta, _ := fm["fibery/meta"].(map[string]any)
				return meta["fibery/collection?"] == true
			}
		}
	}
	return false
}
