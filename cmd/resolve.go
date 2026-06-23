package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	resolveIDOnly bool
	resolveFields []string
)

var resolveCmd = &cobra.Command{
	Use:   "resolve <url>",
	Short: "Fetch any Fibery entity by URL",
	Long: `Fetch a Fibery entity or document by its URL. Handles all entity types.

Examples:
  fibery resolve https://acme.fibery.io/Support_platform/Support_ticket/Fix-signals-5621
  fibery resolve https://acme.fibery.io/Development/Log-queries-1010

Script-friendly (extract the UUID):
  TICKET_UUID=$(fibery resolve <url> --id-only)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, publicID, space, err := resolveURL(args[0])
		if err != nil {
			return err
		}

		if publicID == "" {
			return printSpaceSummary(cmd.Context(), space)
		}

		// If db couldn't be inferred from URL, the URL is the 2-segment /Space/Slug-id
		// form — ambiguous: it may be an entity OR a space/wiki document (both share
		// that shape and can carry the same public id). Resolve both candidates and
		// disambiguate by which one's name matches the URL slug.
		if db == "" {
			slug := normalize(urlTitleSlug(args[0]))

			entity, matchedDB, searchErr := searchSpaceByPublicID(cmd.Context(), space, publicID)
			if searchErr != nil {
				return fmt.Errorf("resolve: could not find entity #%s in space %q: %w", publicID, space, searchErr)
			}
			entityName := ""
			if entity != nil {
				var m map[string]any
				if json.Unmarshal(entity, &m) == nil {
					entityName = normalize(asStr(m["Name"]))
				}
			}

			// Clean entity hit (name matches the slug) → don't consult query-views.
			if entity != nil && entityName == slug {
				if resolveIDOnly {
					return printResolvedID(entity)
				}
				return printEntityLLM(cmd.Context(), entity, matchedDB, "")
			}

			// Ambiguous or no entity: consult the document endpoint. Prefer the
			// document when its name matches the slug, or when there is no entity.
			docV, _ := cli.QueryView(cmd.Context(), publicID)
			if docV != nil && docV.DocumentSecret != "" && (normalize(docV.Name) == slug || entity == nil) {
				if resolveIDOnly {
					fmt.Println(docV.DocumentSecret)
					return nil
				}
				return printDocumentView(cmd.Context(), docV)
			}

			if entity == nil {
				return fmt.Errorf("resolve: #%s not found in space %q (not an entity or space document)", publicID, space)
			}
			if resolveIDOnly {
				return printResolvedID(entity)
			}
			return printEntityLLM(cmd.Context(), entity, matchedDB, "")
		}

		// Build a comprehensive select from schema covering all useful fields
		schema, _ := cache.LoadSchema(account)
		sel, docKeys := buildFullSelect(schema, db)
		if len(resolveFields) > 0 {
			filtered, err := filterSelectByAliases(sel, resolveFields, "fibery/id", "Public ID", "Name")
			if err != nil {
				return err
			}
			sel = filtered
			docKeys = filterDocKeys(docKeys, sel)
		}

		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from":   db,
					"q/select": sel,
					"q/where":  []any{"q/contains-ignoring-case?", []any{"fibery/public-id"}, "$pid"},
					"q/limit":  5,
				},
				"params": map[string]any{"$pid": publicID},
			},
		})
		if err != nil {
			return fmt.Errorf("resolve: query failed for %s#%s: %w", db, publicID, err)
		}

		var items []json.RawMessage
		if err := json.Unmarshal(result, &items); err != nil || len(items) == 0 {
			return fmt.Errorf("resolve: entity %s not found in %s", publicID, db)
		}

		matched := items[0]
		for _, item := range items {
			var m map[string]any
			if json.Unmarshal(item, &m) == nil && asStr(m["PublicID"]) == publicID {
				matched = item
				break
			}
		}

		if resolveIDOnly {
			return printResolvedID(matched)
		}
		return printEntityLLMFull(cmd.Context(), matched, db, docKeys)
	},
}

// printResolvedID prints just the fibery/id UUID for --id-only mode.
func printResolvedID(raw json.RawMessage) error {
	id := extractFiberyID(raw)
	if id == "" {
		return fmt.Errorf("could not extract fibery/id from entity")
	}
	fmt.Println(id)
	return nil
}

// rePublicID matches trailing numeric ID in a URL slug, e.g. "Fix-bug-5621" → "5621"
var rePublicID = regexp.MustCompile(`-(\d+)$`)

// urlTitleSlug returns the title slug from a Fibery URL's last path segment,
// stripped of its trailing "-<publicId>". Used to disambiguate a 2-segment URL
// (which may be an entity or a space document with the same public id) by name.
func urlTitleSlug(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) == 0 {
		return ""
	}
	return rePublicID.ReplaceAllString(segs[len(segs)-1], "")
}

// resolveURL parses a Fibery URL and returns database, publicID, space.
// database may be empty for 2-segment URLs where the db could not be inferred.
func resolveURL(rawURL string) (database, publicID, space string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve: invalid URL: %w", err)
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 {
		return "", "", "", fmt.Errorf("resolve: URL too short — expected at least 2 path segments")
	}

	// Detect space URL: /fibery/space/SpaceName
	if len(segments) >= 3 && segments[0] == "fibery" && segments[1] == "space" {
		return "", "", strings.ReplaceAll(segments[2], "_", " "), nil
	}

	lastSeg := segments[len(segments)-1]
	m := rePublicID.FindStringSubmatch(lastSeg)
	if m == nil {
		return "", "", "", fmt.Errorf("resolve: no public ID found in URL segment %q", lastSeg)
	}
	publicID = m[1]

	switch len(segments) {
	case 3:
		// /{Space_Name}/{Database_Name}/{entity-slug-id}
		space = strings.ReplaceAll(segments[0], "_", " ")
		dbName := strings.ReplaceAll(segments[1], "_", " ")
		database = space + "/" + dbName

	case 2:
		// /{Space}/{entity-slug-id} — infer database from slug, fall back to space-wide search
		space = strings.ReplaceAll(segments[0], "_", " ")
		slug := rePublicID.ReplaceAllString(segments[1], "")
		database, err = inferDatabaseFromSlug(space, slug)
		if err != nil {
			return "", "", space, err
		}
		// database may be "" if no slug match — caller will do space-wide search

	default:
		// >3 segments: first two are space/database
		space = strings.ReplaceAll(segments[0], "_", " ")
		dbName := strings.ReplaceAll(segments[1], "_", " ")
		database = space + "/" + dbName
	}

	return database, publicID, space, nil
}

// searchSpaceByPublicID queries all databases in a space concurrently (batch) for publicID.
func searchSpaceByPublicID(ctx context.Context, space, publicID string) (json.RawMessage, string, error) {
	dbs := spaceDatabases(space)
	if len(dbs) == 0 {
		return nil, "", fmt.Errorf("no databases found in space %q", space)
	}

	schema, _ := cache.LoadSchema(account)

	// Build one command per database
	commands := make([]client.Command, 0, len(dbs))
	for _, db := range dbs {
		commands = append(commands, client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from":   db,
					"q/select": buildSelect(schema, db),
					"q/where":  []any{"q/contains-ignoring-case?", []any{"fibery/public-id"}, "$pid"},
					"q/limit":  1,
				},
				"params": map[string]any{"$pid": publicID},
			},
		})
	}

	results, err := cli.Do(ctx, commands)
	if err != nil {
		return nil, "", err
	}

	for i, r := range results {
		if !r.Success || r.Result == nil {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(r.Result, &items) == nil && len(items) > 0 {
			// Verify exact public ID match
			for _, item := range items {
				var m map[string]any
				if json.Unmarshal(item, &m) == nil && asStr(m["Public ID"]) == publicID {
					return item, dbs[i], nil
				}
			}
		}
	}
	return nil, "", nil
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

// inferDatabaseFromSlug finds the best matching database name for a space + URL slug.
func inferDatabaseFromSlug(space, slug string) (string, error) {
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return "", fmt.Errorf("resolve: schema cache missing — run 'fibery schema sync'")
	}

	target := normalize(slug)
	spaceNorm := normalize(space)

	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name := asStr(tm["fibery/name"])
		if !strings.Contains(name, "/") {
			continue
		}
		parts := strings.SplitN(name, "/", 2)
		if normalize(parts[0]) != spaceNorm {
			continue
		}
		if normalize(parts[1]) == target {
			return name, nil
		}
	}

	// No exact match — return sentinel so caller knows to do a space-wide search
	return "", nil
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

// printEntityLLMFull renders an entity with all fields in LLM-ready Markdown.
// docKeys are keys in the entity whose values are document secrets to fetch and display.
func printEntityLLMFull(ctx context.Context, raw json.RawMessage, db string, docKeys []string) error {
	var entity map[string]any
	if err := json.Unmarshal(raw, &entity); err != nil {
		return err
	}

	// Header: entity name
	if name := asStr(entity["Name"]); name != "" {
		fmt.Fprintf(os.Stdout, "# %s\n\n", strings.TrimSpace(name))
	}
	fmt.Fprintf(os.Stdout, "**Database:** %s\n", db)
	if pid := asStr(entity["Public ID"]); pid != "" && cfg != nil {
		fmt.Fprintf(os.Stdout, "**URL:** %s\n", buildEntityURL(cfg.BaseURL(), db, pid, asStr(entity["Name"])))
	}

	// Field display order
	priorityOrder := []string{"Public ID", "State", "Priority", "Created", "Created By"}
	docKeySet := map[string]bool{}
	for _, k := range docKeys {
		docKeySet[k] = true
	}

	// "fibery/id" is in the JSON output (so scripts can grab the UUID) but hidden
	// from the human-friendly print — the UUID alone isn't useful to read.
	skip := map[string]bool{"Name": true, "fibery/id": true}

	printField := func(k string) {
		if skip[k] || docKeySet[k] {
			return
		}
		v := entity[k]
		if v == nil {
			return
		}
		// Handle complex values (sub-query results)
		switch val := v.(type) {
		case []any:
			if len(val) == 0 {
				return
			}
			var names []string
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					if n := asStr(m["Name"]); n != "" {
						names = append(names, n)
					}
				}
			}
			if len(names) > 0 {
				fmt.Fprintf(os.Stdout, "**%s:** %s\n", k, strings.Join(names, ", "))
			}
		case map[string]any:
			// shouldn't happen for resolved fields
		default:
			s := fmt.Sprintf("%v", v)
			if strings.TrimSpace(s) == "" || s == "false" {
				return
			}
			fmt.Fprintf(os.Stdout, "**%s:** %s\n", k, s)
		}
		skip[k] = true
	}

	// Priority fields first
	for _, k := range priorityOrder {
		printField(k)
	}

	// Remaining fields alphabetically
	var rest []string
	for k := range entity {
		if !skip[k] && !docKeySet[k] {
			rest = append(rest, k)
		}
	}
	for i := 0; i < len(rest)-1; i++ {
		for j := i + 1; j < len(rest); j++ {
			if rest[i] > rest[j] {
				rest[i], rest[j] = rest[j], rest[i]
			}
		}
	}
	for _, k := range rest {
		printField(k)
	}

	// Fetch and display document sections
	for _, key := range docKeys {
		secret := asStr(entity[key])
		if secret == "" {
			continue
		}
		content, err := cli.GetDocument(ctx, secret)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		// Label: strip "_doc_" prefix from key
		label := strings.TrimPrefix(key, "_doc_")
		fmt.Fprintf(os.Stdout, "\n---\n**%s:**\n\n", label)
		fmt.Fprint(os.Stdout, content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}

	return nil
}

// printEntityLLM renders an entity in clean LLM-ready Markdown.
// Internal fields (_secret, ID UUID) are hidden. Document content is appended if non-empty.
func printEntityLLM(ctx context.Context, raw json.RawMessage, db, secret string) error {
	var entity map[string]any
	if err := json.Unmarshal(raw, &entity); err != nil {
		return err
	}

	// Internal / noisy fields to skip in output
	skip := map[string]bool{"_secret": true}

	// Print header: Name first if present, then other fields
	if name := asStr(entity["Name"]); name != "" {
		fmt.Fprintf(os.Stdout, "# %s\n\n", name)
		skip["Name"] = true
	}
	if db != "" {
		fmt.Fprintf(os.Stdout, "**Database:** %s\n", db)
	}

	// Ordered output: PublicID, State, Created, then rest alphabetically
	ordered := []string{"PublicID", "State", "Created"}
	seen := map[string]bool{}
	var remaining []string
	for _, k := range ordered {
		if _, ok := entity[k]; ok && !skip[k] {
			seen[k] = true
		}
	}
	for k := range entity {
		if !skip[k] && !seen[k] {
			remaining = append(remaining, k)
		}
	}
	// stable sort remaining
	for i := 0; i < len(remaining)-1; i++ {
		for j := i + 1; j < len(remaining); j++ {
			if remaining[i] > remaining[j] {
				remaining[i], remaining[j] = remaining[j], remaining[i]
			}
		}
	}
	sortedKeys := append(ordered, remaining...)

	for _, k := range sortedKeys {
		v := entity[k]
		if v == nil || asStr(v) == "" {
			continue
		}
		fmt.Fprintf(os.Stdout, "**%s:** %v\n", k, v)
	}

	// Document content
	if secret != "" {
		content, err := cli.GetDocument(ctx, secret)
		if err == nil && strings.TrimSpace(content) != "" {
			fmt.Fprint(os.Stdout, "\n---\n\n")
			fmt.Fprint(os.Stdout, content)
			if !strings.HasSuffix(content, "\n") {
				fmt.Fprintln(os.Stdout)
			}
		}
	}

	return nil
}

func printSpaceSummary(ctx context.Context, space string) error {
	dbs := spaceDatabases(space)
	if len(dbs) == 0 {
		return fmt.Errorf("no databases found in space %q — run: fibery schema sync", space)
	}
	fmt.Printf("## Space: %s\n\n| Database | Entities |\n|----------|----------|\n", space)
	cmds := make([]client.Command, len(dbs))
	for i, db := range dbs {
		cmds[i] = client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from":   db,
					"q/select": map[string]any{"n": []any{"q/count", "fibery/id"}},
					"q/limit":  1,
				},
			},
		}
	}
	results, err := cli.Do(ctx, cmds)
	for i, db := range dbs {
		count := "—"
		if err == nil && i < len(results) && results[i].Success {
			var rows []map[string]any
			if json.Unmarshal(results[i].Result, &rows) == nil && len(rows) > 0 {
				count = fmt.Sprintf("%v", rows[0]["n"])
			}
		}
		fmt.Printf("| %s | %s |\n", db, count)
	}
	return nil
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

// addCollectionItems appends items to a collection field of an entity.
func addCollectionItems(ctx context.Context, db, entityID, fieldName string, items []any) error {
	_, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/add-collection-items",
		Args: map[string]any{
			"type":   db,
			"field":  fieldName,
			"entity": map[string]any{"fibery/id": entityID},
			"items":  items,
		},
	})
	return err
}

func init() {
	resolveCmd.Flags().BoolVar(&resolveIDOnly, "id-only", false, "output only the fibery/id UUID (for scripting)")
	resolveCmd.Flags().StringSliceVar(&resolveFields, "fields", nil, "comma-separated field aliases to return (saves tokens; e.g. \"Name,State,Priority\")")
	rootCmd.AddCommand(resolveCmd)
}
