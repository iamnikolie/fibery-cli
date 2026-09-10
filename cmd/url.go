package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var urlDB string

var urlCmd = &cobra.Command{
	Use:   "url <id>",
	Short: "Print the canonical Fibery URL for an entity",
	Long: `Print the web URL for an entity, ready to paste into a comment, doc, or Slack.
Accepts UUID, "DT-42", or "42". Requires --db.

Examples:
  fibery url 5651 --db "Development/Dev Task"
  fibery url DT-5651 --db "Development/Dev Task"
  fibery url 550e8400-e29b-41d4-a716-446655440000 --db "Development/Dev Task"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if urlDB == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}
		publicID, name, _, err := fetchIdentity(cmd.Context(), urlDB, args[0])
		if err != nil {
			return err
		}
		if publicID == "" {
			return fmt.Errorf("entity %q in %s has no public id", args[0], urlDB)
		}
		fmt.Println(buildEntityURL(cfg.BaseURL(), urlDB, publicID, name))
		return nil
	},
}

// fetchIdentity resolves an entity reference to its public id, title, and UUID in a
// single query. ref may be a UUID, a bare public id, or a prefixed one ("DT-42").
func fetchIdentity(ctx context.Context, db, ref string) (publicID, name, uuid string, err error) {
	schema, _ := cache.LoadSchema(account)
	sel := map[string]any{
		"PublicID": []any{"fibery/public-id"},
		"UUID":     []any{"fibery/id"},
	}
	if tf := findTitleField(schema, db); tf != "" {
		sel["Name"] = []any{tf}
	}

	cleaned, isU := normalizeEntityRef(ref)
	var where []any
	var params map[string]any
	limit := 1
	if isU {
		where = []any{"=", []any{"fibery/id"}, "$id"}
		params = map[string]any{"$id": cleaned}
	} else {
		where = []any{"q/contains-ignoring-case?", []any{"fibery/public-id"}, "$pid"}
		params = map[string]any{"$pid": cleaned}
		limit = 5
	}

	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": sel,
				"q/where":  where,
				"q/limit":  limit,
			},
			"params": params,
		},
	})
	if err != nil {
		return "", "", "", err
	}

	var items []map[string]any
	if jErr := json.Unmarshal(raw, &items); jErr != nil || len(items) == 0 {
		return "", "", "", fmt.Errorf("entity %q not found in %s", ref, db)
	}
	m := items[0]
	if !isU {
		for _, it := range items {
			if asStr(it["PublicID"]) == cleaned {
				m = it
				break
			}
		}
	}
	return asStr(m["PublicID"]), asStr(m["Name"]), asStr(m["UUID"]), nil
}

// buildEntityURL assembles a canonical Fibery entity URL. Fibery resolves entities
// by the trailing public id, so the title slug is cosmetic; we still include it
// (from the entity name, falling back to the database name) for a readable link.
func buildEntityURL(baseURL, db, publicID, name string) string {
	space, dbName, _ := strings.Cut(db, "/")
	slug := titleToSlug(name)
	if slug == "" {
		slug = titleToSlug(dbName)
	}
	seg := publicID
	if slug != "" {
		seg = slug + "-" + publicID
	}
	return fmt.Sprintf("%s/%s/%s/%s",
		strings.TrimRight(baseURL, "/"), urlSegment(space), urlSegment(dbName), seg)
}

// urlSegment encodes a space or database name as a Fibery URL path segment: spaces
// become underscores (matching how the web app builds and parses entity URLs).
func urlSegment(s string) string {
	return strings.ReplaceAll(s, " ", "_")
}

// titleToSlug converts an entity title into a URL slug: maximal runs of
// non-alphanumeric characters collapse to a single hyphen, and leading/trailing
// hyphens are trimmed.
func titleToSlug(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen && b.Len() > 0 {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func init() {
	urlCmd.Flags().StringVar(&urlDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	rootCmd.AddCommand(urlCmd)
}
