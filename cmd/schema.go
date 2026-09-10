package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Show cached Fibery workspace schema",
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema cached — run: fibery schema sync")
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(schema)
	},
}

var schemaSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Fetch schema from Fibery and update cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := runSchemaSync(cmd.Context()); err != nil {
			return err
		}
		if account == "" {
			fmt.Println("Schema synced to ~/.fibery/schema.json")
		} else {
			fmt.Printf("Schema synced to ~/.fibery/%s/schema.json\n", account)
		}
		return nil
	},
}

// runSchemaSync fetches the Fibery workspace schema and saves it to cache.
// Called from both schemaSyncCmd and PersistentPreRunE in root.go.
func runSchemaSync(ctx context.Context) error {
	result, err := cli.One(ctx, client.Command{
		Command: "fibery.schema/query",
		Args:    map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("schema sync: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(result, &schema); err != nil {
		return fmt.Errorf("schema sync: parse: %w", err)
	}
	return cache.SaveSchema(schema, account)
}

var schemaShowCmd = &cobra.Command{
	Use:     "show <database>",
	Aliases: []string{"fields"},
	Short:   "Show fields for a database",
	Long: `Print a Markdown table of fields (name / type / kind / required) for the given
database. Also available as "fibery schema fields <database>".

Example:
  fibery schema show "Space/Database"
  fibery schema fields "Space/Database"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		db := args[0]
		types, _ := schema["fibery/types"].([]any)
		for _, t := range types {
			tm, ok := t.(map[string]any)
			if !ok || asStr(tm["fibery/name"]) != db {
				continue
			}
			fmt.Printf("## %s\n\n| Field | Type | Kind | Required |\n|-------|------|------|----------|\n", db)
			for _, f := range mustSlice(tm["fibery/fields"]) {
				fm, ok := f.(map[string]any)
				if !ok {
					continue
				}
				name := asStr(fm["fibery/name"])
				typ := asStr(fm["fibery/type"])
				meta, _ := fm["fibery/meta"].(map[string]any)
				req := ""
				if meta["fibery/required?"] == true {
					req = "✓"
				}
				fmt.Printf("| %s | %s | %s | %s |\n", name, typ, fieldKind(typ), req)
			}
			return nil
		}
		return fmt.Errorf("database %q not found — run: fibery schema sync", db)
	},
}

var schemaEnumsCmd = &cobra.Command{
	Use:   "enums <database>",
	Short: "Show enum field values (with fibery/id) for a database",
	Long: `For each enum field in the database, fetch all values and their fibery/id.

Example:
  fibery schema enums "Space/Database"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		db := args[0]
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
				fieldType := asStr(fm["fibery/type"])
				if !isEnumLikeType(fieldType) {
					continue
				}
				fieldName := asStr(fm["fibery/name"])
				result, qErr := cli.One(cmd.Context(), client.Command{
					Command: "fibery.entity/query",
					Args: map[string]any{
						"query": map[string]any{
							"q/from":   fieldType,
							"q/select": map[string]any{"id": "fibery/id", "name": "enum/name"},
							"q/limit":  "q/no-limit",
						},
					},
				})
				if qErr != nil {
					fmt.Fprintf(os.Stderr, "  (could not fetch %s: %v)\n", fieldName, qErr)
					continue
				}
				var vals []map[string]any
				if json.Unmarshal(result, &vals) != nil {
					continue
				}
				fmt.Printf("\n### %s\n\n| Name | fibery/id |\n|------|----------|\n", fieldName)
				for _, v := range vals {
					fmt.Printf("| %s | %s |\n", asStr(v["name"]), asStr(v["id"]))
				}
			}
			return nil
		}
		return fmt.Errorf("database %q not found — run: fibery schema sync", db)
	},
}

func fieldKind(typ string) string {
	switch {
	case isEnumLikeType(typ):
		return "enum"
	case typ == "fibery/user":
		return "user"
	case typ == "Collaboration/Document":
		return "document"
	case strings.Contains(typ, "/"):
		return "relation"
	default:
		return "primitive"
	}
}

func init() {
	schemaCmd.AddCommand(schemaSyncCmd)
	schemaCmd.AddCommand(schemaShowCmd)
	schemaCmd.AddCommand(schemaEnumsCmd)
	rootCmd.AddCommand(schemaCmd)
}
