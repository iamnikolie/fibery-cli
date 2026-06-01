package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/langgerone/fibery-cli/internal/render"
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

var queryParams string

var queryCmd = &cobra.Command{
	Use:   "query <json-query>",
	Short: "Run a raw Fibery FQL query",
	Long: `Run a Fibery FQL query. Pass the query object as JSON.

For parameterised queries use --params to pass variables.
NOTE: params are a sibling of the query object, not inside it.

Param references may use either "$id" (CLI style) or "?id" (native Fibery FQL
style) — both are normalized to "$id" before the call.

Examples:
  fibery query '{"q/from":"Space/Database","q/select":{"ID":["fibery/public-id"],"Name":["Space/Name"]},"q/limit":20}'

  fibery query '{"q/from":"Space/Database","q/select":{"ID":["fibery/public-id"]},"q/where":["=",["fibery/public-id"],"$id"],"q/limit":1}' \
    --params '{"$id":"42"}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var q any
		if err := json.Unmarshal([]byte(args[0]), &q); err != nil {
			return fmt.Errorf("invalid JSON query: %w", err)
		}
		q = normalizeFQLQuery(q)
		cmdArgs := map[string]any{"query": q}
		if queryParams != "" {
			var p map[string]any
			if err := json.Unmarshal([]byte(queryParams), &p); err != nil {
				return fmt.Errorf("invalid --params JSON: %w", err)
			}
			cmdArgs["params"] = normalizeFQLParams(p)
		}
		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args:    cmdArgs,
		})
		if err != nil {
			return err
		}
		return outputJSON(result, func() error {
			return render.List(os.Stdout, result)
		})
	},
}

func init() {
	queryCmd.Flags().StringVar(&queryParams, "params", "", `query params as JSON, e.g. '{"$id":"42"}'`)
	rootCmd.AddCommand(queryCmd)
}
