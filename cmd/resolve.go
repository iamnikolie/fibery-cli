package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
)

var (
	resolveIDOnly bool
	resolveFields []string
	resolveNoDocs bool
	resolveDocs   bool
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
		sel, docKeys, errNarrow := narrowReadSelect(sel, docKeys, resolveFields, resolveNoDocs, "fibery/id", "Public ID", "Name")
		if errNarrow != nil {
			return errNarrow
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
		// Bodies shown only on explicit --docs, or when --fields named a doc field.
		showDocs := resolveDocs || (len(resolveFields) > 0 && len(docKeys) > 0)
		return printEntityLLMFull(cmd.Context(), matched, db, docKeys, showDocs)
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
	resolveCmd.Flags().BoolVar(&resolveDocs, "docs", false, "include rich-text document bodies (Description etc.) — off by default to save tokens")
	resolveCmd.Flags().BoolVar(&resolveNoDocs, "no-docs", false, "also drop document secret keys from --json (rendered bodies are already hidden by default)")
	rootCmd.AddCommand(resolveCmd)
}
