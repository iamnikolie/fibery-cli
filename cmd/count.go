package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/client"
)

var (
	countWhere  string
	countParams string
)

var countCmd = &cobra.Command{
	Use:   "count <database>",
	Short: "Count entities in a database",
	Long: `Count entities in a database, optionally filtered with --where.

Uses a server-side aggregate when the database allows it, and falls back to
paging through ids when the database has row-level permissions (which reject
aggregate queries).

Examples:
  fibery count "Space/Database"
  fibery count "Space/Database" --where '["=",["workflow/state","enum/name"],"$s"]' --params '{"$s":"Open"}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := args[0]

		var where any
		if countWhere != "" {
			clause, err := parseWhereClause(countWhere)
			if err != nil {
				return err
			}
			where = clause
		}
		var params map[string]any
		if countParams != "" {
			if err := json.Unmarshal([]byte(countParams), &params); err != nil {
				return fmt.Errorf("invalid --params JSON: %w", err)
			}
			params = normalizeFQLParams(params)
		}

		n, err := countEntities(cmd.Context(), db, where, params)
		if err != nil {
			return err
		}
		fmt.Println(n)
		return nil
	},
}

// countEntities returns the number of entities in db matching where. It tries a
// server-side q/count aggregate first and falls back to paging through ids when
// the database rejects aggregates (row-level permissions).
func countEntities(ctx context.Context, db string, where any, params map[string]any) (int, error) {
	query := map[string]any{
		"q/from":   db,
		"q/select": map[string]any{"n": []any{"q/count", "fibery/id"}},
		"q/limit":  1,
	}
	if where != nil {
		query["q/where"] = where
	}
	cmdArgs := map[string]any{"query": query}
	if params != nil {
		cmdArgs["params"] = params
	}

	result, err := cli.One(ctx, client.Command{Command: "fibery.entity/query", Args: cmdArgs})
	if err == nil {
		var rows []map[string]any
		if jErr := json.Unmarshal(result, &rows); jErr == nil && len(rows) > 0 {
			switch v := rows[0]["n"].(type) {
			case float64:
				return int(v), nil
			}
		}
		return 0, fmt.Errorf("count: unexpected aggregate result: %s", string(result))
	}
	if !aggregateCountUnsupported(err.Error()) {
		return 0, err
	}

	// Fallback: page through ids and count.
	fmt.Fprintln(os.Stderr, "(counting by paging — this database rejects aggregate queries)")
	idQuery := map[string]any{
		"q/from":     db,
		"q/select":   map[string]any{"id": []any{"fibery/id"}},
		"q/order-by": []any{[]any{[]any{"fibery/public-id"}, "q/asc"}},
	}
	if where != nil {
		idQuery["q/where"] = where
	}
	raw, err := cli.QueryAll(ctx, idQuery, params, 1000)
	if err != nil {
		return 0, err
	}
	var ids []json.RawMessage
	if err := json.Unmarshal(raw, &ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// aggregateCountUnsupported reports whether an error message indicates the database
// does not support aggregate (q/count) queries — the signal to fall back to paging.
func aggregateCountUnsupported(msg string) bool {
	return strings.Contains(msg, "aggregate expressions are not supported") ||
		strings.Contains(msg, "permissions-expr-not-supported")
}

func init() {
	countCmd.Flags().StringVar(&countWhere, "where", "", "FQL where-clause as JSON")
	countCmd.Flags().StringVar(&countParams, "params", "", `query params as JSON, e.g. '{"$s":"Open"}'`)
	rootCmd.AddCommand(countCmd)
}
