package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
)

// rePrefixedID matches "DT-42", "SP-123" etc. and captures just the numeric part.
var rePrefixedID = regexp.MustCompile(`^[A-Za-z]+-(\d+)$`)

var (
	getDB     string
	getIDOnly bool
	getFields []string
	getNoDocs bool
	getDocs   bool
)

var getCmd = &cobra.Command{
	Use:   "get <fibery-id-or-public-id>",
	Short: "Get a Fibery entity by ID",
	Long: `Fetch a single entity. Provide --db with the database name.

Example (by public ID):
  fibery get 42 --db "Development/Dev Task"

Example (by fibery/id UUID):
  fibery get 550e8400-e29b-41d4-a716-446655440000 --db "Development/Dev Task"

Script-friendly (extract the UUID):
  TICKET_UUID=$(fibery get 42 --db "Development/Dev Task" --id-only)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if getDB == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}

		id := args[0]
		// Strip UI prefix: "DT-42" → "42", "SP-123" → "123"
		if m := rePrefixedID.FindStringSubmatch(id); m != nil {
			id = m[1]
		}
		schema, _ := cache.LoadSchema(account)
		sel, docKeys := buildFullSelect(schema, getDB)
		sel, docKeys, errNarrow := narrowReadSelect(sel, docKeys, getFields, getNoDocs, "fibery/id", "Public ID", "Name")
		if errNarrow != nil {
			return errNarrow
		}

		var queryArgs map[string]any
		if isUUID(id) {
			queryArgs = map[string]any{
				"query": map[string]any{
					"q/from":   getDB,
					"q/select": sel,
					"q/where":  []any{"=", []any{"fibery/id"}, "$eid"},
					"q/limit":  1,
				},
				"params": map[string]any{"$eid": id},
			}
		} else {
			queryArgs = map[string]any{
				"query": map[string]any{
					"q/from":   getDB,
					"q/select": sel,
					"q/where":  []any{"q/contains-ignoring-case?", []any{"fibery/public-id"}, "$pid"},
					"q/limit":  5,
				},
				"params": map[string]any{"$pid": id},
			}
		}

		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args:    queryArgs,
		})
		if err != nil {
			return err
		}

		var items []json.RawMessage
		if err := json.Unmarshal(result, &items); err != nil || len(items) == 0 {
			return notFoundRefErr(args[0], getDB)
		}
		// For public ID queries, pick exact match
		matched := items[0]
		if !isUUID(id) {
			for _, item := range items {
				var m map[string]any
				if json.Unmarshal(item, &m) == nil && asStr(m["Public ID"]) == id {
					matched = item
					break
				}
			}
		}
		if getIDOnly {
			id := extractFiberyID(matched)
			if id == "" {
				return fmt.Errorf("could not extract fibery/id from entity")
			}
			fmt.Println(id)
			return nil
		}
		// Bodies shown only on explicit --docs, or when --fields named a doc field.
		showDocs := getDocs || (len(getFields) > 0 && len(docKeys) > 0)
		return outputJSON(matched, func() error {
			return printEntityLLMFull(cmd.Context(), matched, getDB, docKeys, showDocs)
		})
	},
}

// buildSelect returns a field selection for any entity, using the title field from schema.
// Both "ID" (legacy alias) and "fibery/id" return the UUID — the latter so extractFiberyID
// works against any entity built through this select.
func buildSelect(schema map[string]any, db string) map[string]any {
	sel := map[string]any{
		"ID":        []any{"fibery/id"},
		"fibery/id": []any{"fibery/id"},
		"Public ID": []any{"fibery/public-id"},
		"Created":   []any{"fibery/creation-date"},
		"State":     []any{"workflow/state", "enum/name"},
	}
	if nameField := findTitleField(schema, db); nameField != "" {
		sel["Name"] = []any{nameField}
	}
	return sel
}

// notFoundRefErr formats an entity-not-found error that names the accepted ID
// forms on one line, so agents stop thrashing between public / prefixed / UUID
// inputs. It does not dump the schema.
func notFoundRefErr(ref, db string) error {
	return fmt.Errorf("entity %q not found in %s — accepted: public id \"42\", prefixed \"DT-42\", a UUID, or 'fibery resolve <url>'", ref, db)
}

// isUUID returns true if s looks like a UUID.
func isUUID(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}

// rePublicIDOnly matches a bare numeric public ID like "42".
var rePublicIDOnly = regexp.MustCompile(`^\d+$`)

// normalizeEntityRef classifies a user-supplied entity reference. Returns the
// cleaned reference and whether it is already a UUID (no lookup needed).
//   - UUID → passthrough, isUUID=true
//   - "DT-42" → "42", isUUID=false
//   - "42" → "42", isUUID=false
//   - anything else → passthrough, isUUID=false (lookup will fail with a clear error)
func normalizeEntityRef(ref string) (string, bool) {
	if isUUID(ref) {
		return ref, true
	}
	if m := rePrefixedID.FindStringSubmatch(ref); m != nil {
		return m[1], false
	}
	return ref, false
}

// looksLikeEntityRef returns true when the input has the shape of a UUID,
// a bare numeric ID, or a prefixed numeric ID. Used to decide whether to
// treat ambiguous inputs (e.g. doc commands) as an entity ref vs a literal
// document secret.
func looksLikeEntityRef(s string) bool {
	if isUUID(s) {
		return true
	}
	if rePublicIDOnly.MatchString(s) {
		return true
	}
	if rePrefixedID.MatchString(s) {
		return true
	}
	return false
}

// resolveEntityRef returns the fibery/id UUID for a user-supplied reference.
// Accepts UUID (passthrough), "42" (public-id lookup), or "DT-42" (prefix
// stripped, then public-id lookup).
func resolveEntityRef(ctx context.Context, db, ref string) (string, error) {
	cleaned, ok := normalizeEntityRef(ref)
	if ok {
		return cleaned, nil
	}
	result, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": map[string]any{"id": "fibery/id"},
				"q/where":  []any{"=", []any{"fibery/public-id"}, "$pid"},
				"q/limit":  1,
			},
			"params": map[string]any{"$pid": cleaned},
		},
	})
	if err != nil {
		return "", fmt.Errorf("resolve entity %q in %s: %w", ref, db, err)
	}
	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil {
		return "", fmt.Errorf("resolve entity %q in %s: parse: %w", ref, db, err)
	}
	if len(items) == 0 {
		return "", notFoundRefErr(ref, db)
	}
	return asStr(items[0]["id"]), nil
}

// narrowReadSelect applies the --fields and --no-docs read modifiers to a full
// entity select and its document keys, returning the narrowed select plus the
// surviving doc keys. It is shared by get and resolve.
//
// --fields wins: when fields are given they fully determine the selection (a
// rich-text field survives only if explicitly named), so --no-docs adds nothing
// and is intentionally a no-op in that case — avoiding a contradiction error.
// With no --fields, --no-docs drops every rich-text document body from the
// select so the (potentially large) bodies are never queried or fetched.
func narrowReadSelect(sel map[string]any, docKeys []string, fields []string, noDocs bool, alwaysKeep ...string) (map[string]any, []string, error) {
	if len(fields) > 0 {
		filtered, err := filterSelectByAliases(sel, fields, alwaysKeep...)
		if err != nil {
			return nil, nil, err
		}
		return filtered, filterDocKeys(docKeys, filtered), nil
	}
	if noDocs {
		sel, docKeys = dropDocKeys(sel, docKeys)
	}
	return sel, docKeys, nil
}

// dropDocKeys returns a copy of sel with every document-body key removed, and a
// nil doc-key slice. Used by --no-docs to suppress rich-text bodies.
func dropDocKeys(sel map[string]any, docKeys []string) (map[string]any, []string) {
	if len(docKeys) == 0 {
		return sel, docKeys
	}
	drop := make(map[string]bool, len(docKeys))
	for _, k := range docKeys {
		drop[k] = true
	}
	out := make(map[string]any, len(sel))
	for k, v := range sel {
		if drop[k] {
			continue
		}
		out[k] = v
	}
	return out, nil
}

// filterDocKeys drops doc keys whose select entry was filtered out so we don't
// try to fetch a document we no longer have a secret for.
func filterDocKeys(docKeys []string, sel map[string]any) []string {
	out := docKeys[:0]
	for _, k := range docKeys {
		if _, ok := sel[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// filterSelectByAliases narrows a q/select to the keys the user asked for via
// --fields. Aliases match case-insensitively; document keys (prefixed "_doc_")
// match by their stripped name. alwaysKeep is the set of keys preserved
// regardless of the filter (e.g. "fibery/id" so --id-only still works).
// When aliases is empty the input is returned unchanged.
func filterSelectByAliases(sel map[string]any, aliases []string, alwaysKeep ...string) (map[string]any, error) {
	if len(aliases) == 0 {
		return sel, nil
	}
	// Build a lookup from lowercased alias → real select key. For document
	// keys we index both the raw "_doc_X" and the stripped "X".
	lookup := map[string]string{}
	for k := range sel {
		lookup[strings.ToLower(k)] = k
		if stripped, ok := strings.CutPrefix(k, "_doc_"); ok {
			lookup[strings.ToLower(stripped)] = k
		}
	}
	keep := map[string]bool{}
	for _, k := range alwaysKeep {
		if _, ok := sel[k]; ok {
			keep[k] = true
		}
	}
	var unknown []string
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		real, ok := lookup[strings.ToLower(a)]
		if !ok {
			unknown = append(unknown, a)
			continue
		}
		keep[real] = true
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown --fields: %s (run 'fibery schema show <db>' to see available fields)", strings.Join(unknown, ", "))
	}
	out := make(map[string]any, len(keep))
	for k := range keep {
		out[k] = sel[k]
	}
	return out, nil
}

// extractFiberyID returns the "fibery/id" value from a raw JSON entity object,
// or "" if missing or unparseable. Used by --id-only flags across commands.
func extractFiberyID(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	id, ok := m["fibery/id"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(id, &s); err != nil {
		return strings.Trim(string(id), `"`)
	}
	return s
}

func init() {
	getCmd.Flags().StringVar(&getDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	getCmd.Flags().BoolVar(&getIDOnly, "id-only", false, "output only the fibery/id UUID (for scripting)")
	getCmd.Flags().StringSliceVar(&getFields, "fields", nil, "comma-separated field aliases to return (saves tokens; e.g. \"Name,State,Priority\")")
	getCmd.Flags().BoolVar(&getDocs, "docs", false, "include rich-text document bodies (Description etc.) — off by default to save tokens")
	getCmd.Flags().BoolVar(&getNoDocs, "no-docs", false, "also drop document secret keys from --json (rendered bodies are already hidden by default)")
	rootCmd.AddCommand(getCmd)
}
