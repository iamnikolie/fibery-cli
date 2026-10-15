package cmd

import (
	"sort"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/profile"
)

// strippedName returns the field name without its "Space/" prefix, lowercased.
func strippedName(field string) string {
	if i := strings.Index(field, "/"); i >= 0 {
		field = field[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(field))
}

// edgeRole maps a relation field to a graph role, or "" if it is not a tracked
// edge. Precedence matters: "merge request"/"bug"/"service" win before the
// generic block/related and dev-task/dev-epic checks so e.g. a "bug" relation is
// never mistaken for a parent.
func edgeRole(name string, collection bool) string {
	n := strippedName(name)
	switch {
	case strings.Contains(n, "merge request"):
		return "code"
	case strings.Contains(n, "bug"):
		return "quality"
	case n == "services":
		return "service"
	case strings.Contains(n, "block") || strings.Contains(n, "related"):
		return "blockers"
	case collection && (strings.Contains(n, "dev task") || strings.Contains(n, "dev epic")):
		return "children"
	case !collection && (strings.Contains(n, "dev epic") || n == "story" || n == "epic" || strings.Contains(n, "story from epic")):
		return "parent"
	default:
		return ""
	}
}

// docAllowed reports whether a document field should be inlined by default.
// Allowlist by stripped name; exclude the lookup-mirror docs (Story/Epic
// Description duplicate neighbor content reached via edges) and release-time
// epic checklist docs ("1) …", "Release notes").
func docAllowed(name string) bool {
	n := strippedName(name)
	if strings.Contains(n, "story description") || strings.Contains(n, "epic description") {
		return false
	}
	if strings.Contains(n, "release notes") || (len(n) > 1 && n[0] >= '1' && n[0] <= '9' && n[1] == ')') {
		return false
	}
	return strings.Contains(n, "description") ||
		strings.Contains(n, "knowledge transfer") ||
		strings.Contains(n, "qa summary")
}

// isEntityRelationType reports whether a field type names another entity
// database (Space/Database) rather than a primitive, enum-like, or system type.
func isEntityRelationType(typ string) bool {
	if !strings.Contains(typ, "/") {
		return false
	}
	if isEnumLikeType(typ) {
		return false
	}
	switch typ {
	case "fibery/user", "Collaboration~Documents/Document", "Collaboration~Documents/Reference":
		return false
	}
	if strings.HasPrefix(typ, "fibery/") {
		return false
	}
	return true
}

// classifyProfile builds a draft Profile for db from the cached schema, plus a
// sorted list of relation fields it could not classify (for the draft's review
// comment block). It mirrors buildFullSelect's type handling but emits field
// names (profile data) rather than a q/select.
func classifyProfile(schema map[string]any, db string) (profile.Profile, []string) {
	prof := profile.Profile{Comments: "full"}
	var unclassified []string

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
			name := asStr(fm["fibery/name"])
			typ := asStr(fm["fibery/type"])
			meta, _ := fm["fibery/meta"].(map[string]any)
			collection := meta["fibery/collection?"] == true
			isTitle := meta["ui/title?"] == true

			switch {
			case isTitle:
				prof.Keep = append(prof.Keep, name)
			case name == "workflow/state":
				prof.Keep = append(prof.Keep, name)
			case typ == "fibery/user":
				prof.Keep = append(prof.Keep, name) // assignees, created-by
			case name == "fibery/creation-date" || name == "fibery/modification-date":
				prof.Keep = append(prof.Keep, name)
			case typ == "Collaboration~Documents/Document":
				if docAllowed(name) {
					prof.Docs = append(prof.Docs, name)
				}
			case isEnumLikeType(typ):
				// Keep priority-ish enums; drop other workspace enums (tags/teams).
				if strings.Contains(strippedName(name), "priority") {
					prof.Keep = append(prof.Keep, name)
				}
			case isEntityRelationType(typ):
				switch edgeRole(name, collection) {
				case "parent":
					prof.Edges.Parent = append(prof.Edges.Parent, name)
				case "children":
					prof.Edges.Children = append(prof.Edges.Children, name)
				case "blockers":
					prof.Edges.Blockers = append(prof.Edges.Blockers, name)
				case "code":
					prof.Edges.Code = append(prof.Edges.Code, name)
				case "quality":
					prof.Edges.Quality = append(prof.Edges.Quality, name)
				case "service":
					prof.Edges.Service = append(prof.Edges.Service, name)
				default:
					unclassified = append(unclassified, name)
				}
			default:
				// primitive int/decimal/bool/date-range, *_deleted, rank, files,
				// views, references → dropped (noise).
			}
		}
		break
	}
	sort.Strings(unclassified)
	return prof, unclassified
}
