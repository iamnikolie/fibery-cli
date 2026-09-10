package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var stateDB string

var stateCmd = &cobra.Command{
	Use:   "state <id> <state-name>",
	Short: "Change the workflow state of an entity",
	Long: `Change workflow state. Requires --db. Accepts UUID, "DT-42", or "42".

Examples:
  fibery state 42 "In Progress" --db "Development/bug"
  fibery state DT-42 "In Progress" --db "Development/bug"
  fibery state 550e8400-e29b-41d4-a716-446655440000 "In Progress" --db "Development/bug"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if stateDB == "" {
			return fmt.Errorf("--db is required")
		}
		entityID, err := resolveEntityRef(cmd.Context(), stateDB, args[0])
		if err != nil {
			return err
		}
		stateName := args[1]

		// Look up the state UUID by querying the workflow state type directly
		stateID, err := resolveStateUUID(cmd, stateDB, stateName)
		if err != nil {
			return err
		}

		_, err = cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/update",
			Args: map[string]any{
				"type": stateDB,
				"entity": map[string]any{
					"fibery/id":      entityID,
					"workflow/state": map[string]any{"fibery/id": stateID},
				},
			},
		})
		if err != nil {
			return err
		}
		fmt.Printf("State set to %q\n", stateName)
		return nil
	},
}

// resolveStateUUID queries Fibery for the state UUID matching stateName in db's workflow.
func resolveStateUUID(cmd *cobra.Command, db, stateName string) (string, error) {
	// Find the workflow state type name from schema
	schema, _ := cache.LoadSchema(account)
	stateTypeName := ""
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
			if asStr(fm["fibery/name"]) == "workflow/state" {
				stateTypeName = asStr(fm["fibery/type"])
				break
			}
		}
		break
	}
	if stateTypeName == "" {
		return "", fmt.Errorf("state: database %q has no workflow/state field", db)
	}

	// Query all states from Fibery directly
	result, err := cli.One(cmd.Context(), client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   stateTypeName,
				"q/select": map[string]any{"id": "fibery/id", "name": "enum/name"},
				"q/limit":  "q/no-limit",
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("state: fetch states: %w", err)
	}

	var states []map[string]any
	if err := json.Unmarshal(result, &states); err != nil {
		return "", fmt.Errorf("state: parse states: %w", err)
	}

	var available []string
	for _, s := range states {
		name := asStr(s["name"])
		available = append(available, name)
		if strings.EqualFold(name, stateName) {
			return asStr(s["id"]), nil
		}
	}
	return "", fmt.Errorf("state %q not found in %q.\nAvailable: %s",
		stateName, db, strings.Join(available, ", "))
}

// findStateUUID looks up the UUID for a workflow state by name in the schema cache.

func init() {
	stateCmd.Flags().StringVar(&stateDB, "db", "", "database name (e.g. \"Development/bug\")")
	rootCmd.AddCommand(stateCmd)
}
