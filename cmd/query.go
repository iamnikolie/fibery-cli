package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"

	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/langgerone/fibery-cli/internal/render"
	"github.com/spf13/cobra"
)

// reFQLParamRef matches a bare "?identifier" string — what Fibery FQL natively
// uses for a parameter reference. The CLI canonicalizes to "$identifier" since
// raw "?" anywhere in the query produces a server-side regex crash.
var reFQLParamRef = regexp.MustCompile(`^\?[A-Za-z_][A-Za-z0-9_-]*$`)

// normalizeFQLParams rewrites top-level "?xxx" keys to "$xxx" so users can pass
// either prefix in --params.
func normalizeFQLParams(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if len(k) > 1 && k[0] == '?' {
			k = "$" + k[1:]
		}
		out[k] = v
	}
	return out
}

// normalizeFQLQuery walks the parsed FQL query and rewrites any bare "?xxx"
// string value to "$xxx" so a param reference in either style works.
// Strings with embedded punctuation (search text, operator names like
// "q/contains-ignoring-case?") are left untouched.
func normalizeFQLQuery(v any) any {
	switch x := v.(type) {
	case string:
		if reFQLParamRef.MatchString(x) {
			return "$" + x[1:]
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeFQLQuery(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = normalizeFQLQuery(item)
		}
		return out
	default:
		return x
	}
}

// parseWhereClause parses a JSON FQL where-clause string and normalizes any "?x"
// param references to "$x". Shared by query, list, and count.
func parseWhereClause(s string) (any, error) {
	var clause any
	if err := json.Unmarshal([]byte(s), &clause); err != nil {
		return nil, fmt.Errorf("invalid --where JSON: %w", err)
	}
	return normalizeFQLQuery(clause), nil
}

var (
	queryParams  string
	queryDB      string
	querySelect  []string
	queryWhere   string
	queryFilters []string
	queryLimit   int
	queryOrder   string
	queryAll     bool
)

var queryCmd = &cobra.Command{
	Use:   "query [json-query]",
	Short: "Run a Fibery FQL query (raw JSON, or built from flags)",
	Long: `Run a Fibery FQL query. Two modes:

  1. Raw FQL — pass the query object as JSON (full power):
       fibery query '{"q/from":"Space/Database","q/select":{"ID":["fibery/public-id"]},"q/limit":20}'

  2. Builder — omit the JSON and use flags (no FQL needed):
       fibery query --db "Space/Database" --select "Space/Name" --limit 5
       fibery query --db "Space/Database" --where '["=",["workflow/state","enum/name"],"$s"]' --params '{"$s":"Open"}'
       fibery query --db "Space/Database" --all      # stream every row, past the 3001 cap

For parameterised queries use --params (a sibling of the query object, not inside it).
Param references may use "$id" (CLI style) or "?id" (native FQL) — both normalize to "$id".
--all pages with q/offset to return every matching row.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query, extraParams, err := buildQuery(args)
		if err != nil {
			return err
		}

		var params map[string]any
		if queryParams != "" {
			if err := json.Unmarshal([]byte(queryParams), &params); err != nil {
				return fmt.Errorf("invalid --params JSON: %w", err)
			}
			params = normalizeFQLParams(params)
		}
		if len(extraParams) > 0 {
			if params == nil {
				params = map[string]any{}
			}
			maps.Copy(params, extraParams)
		}

		var result json.RawMessage
		if queryAll {
			q, ok := query.(map[string]any)
			if !ok {
				return fmt.Errorf("--all requires a query object")
			}
			if _, has := q["q/order-by"]; !has {
				q["q/order-by"] = []any{[]any{[]any{"fibery/public-id"}, "q/asc"}}
			}
			result, err = cli.QueryAll(cmd.Context(), q, params, 1000)
		} else {
			cmdArgs := map[string]any{"query": query}
			if params != nil {
				cmdArgs["params"] = params
			}
			result, err = cli.One(cmd.Context(), client.Command{Command: "fibery.entity/query", Args: cmdArgs})
		}
		if err != nil {
			return err
		}
		return outputJSON(result, func() error {
			return render.List(os.Stdout, result)
		})
	},
}

// buildQuery returns the FQL query object (from a positional JSON argument or
// assembled from the builder flags) plus any params it generated (--filter emits
// $flt* params that must travel alongside the query).
func buildQuery(args []string) (any, map[string]any, error) {
	if len(args) == 1 {
		if queryDB != "" {
			return nil, nil, fmt.Errorf("pass either a JSON query OR the --db builder flags, not both")
		}
		var q any
		if err := json.Unmarshal([]byte(args[0]), &q); err != nil {
			return nil, nil, fmt.Errorf("invalid JSON query: %w", err)
		}
		return normalizeFQLQuery(q), nil, nil
	}

	if queryDB == "" {
		return nil, nil, fmt.Errorf("provide a JSON query, or use --db (with optional --select/--where/--filter/--limit/--order)")
	}

	schema, _ := cache.LoadSchema(account)
	q := map[string]any{"q/from": queryDB}

	if len(querySelect) > 0 {
		sel := map[string]any{}
		for _, f := range querySelect {
			f = resolveFieldName(schema, queryDB, f) // case-insensitive correction
			sel[fieldAlias(f)] = []any{f}
		}
		q["q/select"] = sel
	} else {
		q["q/select"] = buildSelect(schema, queryDB)
	}

	var extraParams map[string]any
	if len(queryFilters) > 0 {
		if queryWhere != "" {
			return nil, nil, fmt.Errorf("use either --where or --filter, not both")
		}
		clause, fparams, ferr := buildFilterClause(schema, queryDB, queryFilters)
		if ferr != nil {
			return nil, nil, ferr
		}
		q["q/where"] = clause
		extraParams = fparams
	} else if queryWhere != "" {
		clause, err := parseWhereClause(queryWhere)
		if err != nil {
			return nil, nil, err
		}
		q["q/where"] = clause
	}

	if queryOrder != "" {
		field, desc := resolveSortField(queryOrder)
		dir := "q/asc"
		if desc {
			dir = "q/desc"
		}
		q["q/order-by"] = []any{[]any{[]any{field}, dir}}
	}

	if !queryAll {
		q["q/limit"] = queryLimit
	}
	return q, extraParams, nil
}

func init() {
	queryCmd.Flags().StringVar(&queryParams, "params", "", `query params as JSON, e.g. '{"$id":"42"}'`)
	queryCmd.Flags().StringVar(&queryDB, "db", "", "database name (builder mode — assembles the FQL for you)")
	queryCmd.Flags().StringSliceVar(&querySelect, "select", nil, "comma-separated field names to select (builder mode)")
	queryCmd.Flags().StringVar(&queryWhere, "where", "", "FQL where-clause as JSON (builder mode)")
	queryCmd.Flags().StringArrayVar(&queryFilters, "filter", nil, `simple filter "field=value" (builder mode; repeatable; ops: = != ~)`)
	queryCmd.Flags().IntVar(&queryLimit, "limit", 50, "max results (builder mode; ignored with --all)")
	queryCmd.Flags().StringVar(&queryOrder, "order", "", `sort field: "created", "modified", "-created", or any field name (builder mode)`)
	queryCmd.Flags().BoolVar(&queryAll, "all", false, "return every matching row, paging past the 3001-row cap")
	rootCmd.AddCommand(queryCmd)
}
