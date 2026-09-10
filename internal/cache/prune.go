package cache

// deletedKey is the schema flag Fibery sets on a type or field that the
// workspace has removed. The server keeps the tombstone in the schema forever
// (renamed to "<original name>_<hash>_deleted"), so it must be filtered on the
// client side.
const deletedKey = "fibery/deleted?"

// isDeleted reports whether a schema type/field node is a deletion tombstone.
// A node without the flag counts as live, so an older cached schema that
// predates the flag keeps working unchanged.
func isDeleted(node map[string]any) bool {
	v, _ := node[deletedKey].(bool)
	return v
}

// pruneDeleted strips deletion tombstones from a parsed Fibery schema, in
// place. It removes, in this order:
//
//  1. types flagged fibery/deleted?,
//  2. fields flagged fibery/deleted?,
//  3. surviving fields whose fibery/type points at a type removed by (1).
//
// Without this every schema consumer treats a tombstone as a real field. That
// is not merely noisy: buildFullSelect puts the ghost into q/select, and a
// relation to a deleted type is navigated as if it were an enum, which the API
// rejects with entity.error/schema-field-not-found — so a single tombstone
// breaks `fibery get` for the whole database.
//
// The flag is used rather than the "_<hash>_deleted" name suffix because it is
// authoritative on both counts: fields inside a deleted type keep their
// ordinary names yet are flagged, and a live field legitimately named
// "..._deleted" is not.
func pruneDeleted(schema map[string]any) {
	types, ok := schema["fibery/types"].([]any)
	if !ok {
		return
	}

	deletedTypes := make(map[string]bool)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || !isDeleted(tm) {
			continue
		}
		name, _ := tm["fibery/name"].(string)
		deletedTypes[name] = true
	}

	keptTypes := types[:0]
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			keptTypes = append(keptTypes, t)
			continue
		}
		if isDeleted(tm) {
			continue
		}
		if fields, ok := tm["fibery/fields"].([]any); ok {
			tm["fibery/fields"] = pruneFields(fields, deletedTypes)
		}
		keptTypes = append(keptTypes, t)
	}
	schema["fibery/types"] = keptTypes
}

// pruneFields returns fields minus the tombstones and minus every field whose
// type was itself deleted. Filtering is in place over the input slice.
func pruneFields(fields []any, deletedTypes map[string]bool) []any {
	kept := fields[:0]
	for _, f := range fields {
		fm, ok := f.(map[string]any)
		if !ok {
			kept = append(kept, f)
			continue
		}
		if isDeleted(fm) {
			continue
		}
		if ft, _ := fm["fibery/type"].(string); deletedTypes[ft] {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}
