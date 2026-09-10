package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/iamnikolie/fibery-cli/internal/render"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// isTTY returns true when stdout is connected to an interactive terminal.
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

var (
	searchDB    string
	searchLimit int
)

var searchCmd = &cobra.Command{
	Use:   "search <text>",
	Short: "Search entities by name within a database",
	Long: `Search entities whose name contains the given text.
If --db is omitted, shows an interactive database picker.

Examples:
  fibery search "webhook"
  fibery search "webhook" --db "Support platform/Support ticket"
  fibery search "auth" --db "Development/bug" --limit 10`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := searchDB
		if db == "" {
			if !isTTY() {
				// Non-interactive (e.g. Claude Code): list available databases
				schema, _ := cache.LoadSchema(account)
				dbs := listDatabases(schema)
				return fmt.Errorf("--db is required. Available databases:\n%s\n\nExample: fibery search %q --db \"Support platform/Support ticket\"",
					strings.Join(dbs, "\n"), args[0])
			}
			var err error
			db, err = pickDatabase()
			if err != nil {
				return err
			}
		}

		schema, _ := cache.LoadSchema(account)
		titleField := findTitleField(schema, db)
		if titleField == "" {
			return fmt.Errorf("search: could not find title field for %q — run: fibery schema sync", db)
		}

		sel := map[string]any{
			"ID":   []any{"fibery/public-id"},
			"Name": []any{titleField},
		}
		if schemaHasField(schema, db, "workflow/state") {
			sel["State"] = []any{"workflow/state", "enum/name"}
		}

		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from":     db,
					"q/select":   sel,
					"q/where":    []any{"q/contains-ignoring-case?", []any{titleField}, "$q"},
					"q/order-by": []any{[]any{[]any{"fibery/creation-date"}, "q/desc"}},
					"q/limit":    searchLimit,
				},
				"params": map[string]any{"$q": args[0]},
			},
		})
		if err != nil {
			return err
		}
		paginationHint(os.Stderr, result, searchLimit)
		return outputJSON(result, func() error {
			return render.List(os.Stdout, result)
		})
	},
}

// pickDatabase shows an interactive searchable list of databases from the schema cache.
func pickDatabase() (string, error) {
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return "", fmt.Errorf("schema cache missing — run: fibery schema sync")
	}

	dbs := listDatabases(schema)
	if len(dbs) == 0 {
		return "", fmt.Errorf("no databases found in schema — run: fibery schema sync")
	}

	prompt := promptui.Select{
		Label: "Select database",
		Items: dbs,
		Size:  15,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(dbs[index]), strings.ToLower(input))
		},
		StartInSearchMode: true,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}",
			Active:   "▶ {{ . | cyan }}",
			Inactive: "  {{ . }}",
			Selected: "▶ {{ . | green }}",
		},
	}

	_, selected, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("cancelled")
	}
	return selected, nil
}

// listDatabases returns all non-platform, non-enum database names from the schema.
func listDatabases(schema map[string]any) []string {
	types, _ := schema["fibery/types"].([]any)
	var dbs []string
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name := asStr(tm["fibery/name"])
		if !strings.Contains(name, "/") {
			continue
		}
		meta, _ := tm["fibery/meta"].(map[string]any)
		if meta["fibery/platform?"] == true || meta["fibery/primitive?"] == true {
			continue
		}
		// Skip enum/helper types (contain _ after the space separator)
		parts := strings.SplitN(name, "/", 2)
		if strings.Contains(parts[1], "_") {
			continue
		}
		dbs = append(dbs, name)
	}
	return dbs
}

func init() {
	searchCmd.Flags().StringVar(&searchDB, "db", "", "database to search in (interactive picker if omitted)")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 20, "max results")
	rootCmd.AddCommand(searchCmd)
}
