package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/langgerone/fibery-cli/internal/render"
	"github.com/spf13/cobra"
)

// countJSONArray returns the length of a top-level JSON array, or 0 if the
// payload isn't an array.
func countJSONArray(raw json.RawMessage) int {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	return len(items)
}

// paginationHint writes a one-line stderr note when the result count hit the
// requested --limit, signaling that there may be more.
func paginationHint(w io.Writer, raw json.RawMessage, limit int) {
	n := countJSONArray(raw)
	if n > 0 && n >= limit {
		fmt.Fprintf(w, "(showing %d results — limit reached; pass --limit %d for more)\n", n, limit*2)
	}
}

var (
	listLimit   int
	listSort    string
	listFields  []string
	listWhere   string
	listFilters []string
	listAll     bool
	listParams  string
)

var sortAliases = map[string]string{
	"created":  "fibery/creation-date",
	"modified": "fibery/modification-date",
}

// resolveSortField returns the Fibery field name and whether to sort descending.
// Prefix with "-" for descending. Aliases: "created", "modified".
func resolveSortField(s string) (field string, desc bool) {
	if strings.HasPrefix(s, "-") {
		s = s[1:]
		desc = true
	}
	if mapped, ok := sortAliases[s]; ok {
		return mapped, desc
	}
	return s, desc
}

var listCmd = &cobra.Command{
	Use:   "list <database>",
	Short: "List entities in a database",
	Long: `List entities in a database. Shows ID, Name, and State (if the database has a workflow).

Examples:
  fibery list "Space/Database"
  fibery list "Space/Database" --limit 100 --sort -modified
  fibery list "Space/Database" --sort "Space/Priority"

  # --filter builds the where-clause for you (no FQL). Repeatable; ANDed.
  # Children of an epic (relation matched by public id):
  fibery list "Development/Dev Task" --filter "Development/Dev epic=2319"
  # Combine: tasks in an epic that are not yet done (enum matched by name)
  fibery list "Development/Dev Task" --filter "Development/Dev epic=2319" --filter "workflow/state!=Done"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := args[0]
		schema, _ := cache.LoadSchema(account)

		var sel map[string]any
		if len(listFields) > 0 {
			// User asked for specific fields — start from the full schema-driven
			// select and filter. Always keep ID and Name so rows stay identifiable.
			full, _ := buildFullSelect(schema, db)
			filtered, err := filterSelectByAliases(full, listFields, "Public ID", "Name")
			if err != nil {
				return err
			}
			sel = filtered
		} else {
			titleField := findTitleField(schema, db)
			sel = map[string]any{
				"ID": []any{"fibery/public-id"},
			}
			if titleField != "" {
				sel["Name"] = []any{titleField}
			}
			if schemaHasField(schema, db, "workflow/state") {
				sel["State"] = []any{"workflow/state", "enum/name"}
			}
		}

		sortField, sortDesc := resolveSortField(listSort)
		dir := "q/asc"
		if sortDesc {
			dir = "q/desc"
		}

		query := map[string]any{
			"q/from":     db,
			"q/select":   sel,
			"q/order-by": []any{[]any{[]any{sortField}, dir}},
		}

		var params map[string]any
		if listParams != "" {
			if perr := json.Unmarshal([]byte(listParams), &params); perr != nil {
				return fmt.Errorf("invalid --params JSON: %w", perr)
			}
			params = normalizeFQLParams(params)
		}

		if len(listFilters) > 0 {
			if listWhere != "" {
				return fmt.Errorf("use either --where or --filter, not both")
			}
			clause, fparams, ferr := buildFilterClause(schema, db, listFilters)
			if ferr != nil {
				return ferr
			}
			query["q/where"] = clause
			if params == nil {
				params = map[string]any{}
			}
			maps.Copy(params, fparams)
		} else if listWhere != "" {
			clause, werr := parseWhereClause(listWhere)
			if werr != nil {
				return werr
			}
			query["q/where"] = clause
		}

		var result json.RawMessage
		var err error
		if listAll {
			result, err = cli.QueryAll(cmd.Context(), query, params, 1000)
		} else {
			query["q/limit"] = listLimit
			listArgs := map[string]any{"query": query}
			if params != nil {
				listArgs["params"] = params
			}
			result, err = cli.One(cmd.Context(), client.Command{
				Command: "fibery.entity/query",
				Args:    listArgs,
			})
		}
		if err != nil {
			return err
		}
		if !listAll {
			paginationHint(os.Stderr, result, listLimit)
		}
		return outputJSON(result, func() error {
			return render.List(os.Stdout, result)
		})
	},
}

func init() {
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "max results")
	listCmd.Flags().StringVar(&listSort, "sort", "-created", `sort field: "created", "modified", "-created", "-modified", or any Fibery field name`)
	listCmd.Flags().StringSliceVar(&listFields, "fields", nil, "comma-separated field aliases to return (e.g. \"Name,State,Priority\")")
	listCmd.Flags().StringVar(&listWhere, "where", "", "FQL where-clause as JSON (e.g. '[\"=\",[\"workflow/state\",\"enum/name\"],\"$s\"]')")
	listCmd.Flags().StringArrayVar(&listFilters, "filter", nil, `simple filter "field=value" (repeatable; ops: = != ~). Relations match by public id, enums by name, users by email. e.g. --filter "Development/Dev epic=2319"`)
	listCmd.Flags().StringVar(&listParams, "params", "", `query params as JSON for --where, e.g. '{"$s":"Open"}'`)
	listCmd.Flags().BoolVar(&listAll, "all", false, "return every matching row, paging past the 3001-row cap")
	rootCmd.AddCommand(listCmd)
}
