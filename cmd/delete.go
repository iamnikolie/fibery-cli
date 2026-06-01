package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/client"
)

var (
	deleteDB  string
	deleteYes bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a Fibery entity",
	Long: `Delete an entity. Requires --db and --yes (the latter is mandatory because
delete is destructive and irreversible). Accepts UUID, "DT-42", or "42".

Example:
  fibery delete 42 --db "Development/Dev Task" --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if deleteDB == "" {
			return fmt.Errorf("--db is required")
		}
		if !deleteYes {
			return fmt.Errorf("delete is destructive — add --yes to confirm (entity %q in %s)", args[0], deleteDB)
		}
		entityID, err := resolveEntityRef(cmd.Context(), deleteDB, args[0])
		if err != nil {
			return err
		}
		_, err = cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/delete",
			Args: map[string]any{
				"type":   deleteDB,
				"entity": map[string]any{"fibery/id": entityID},
			},
		})
		if err != nil {
			return err
		}
		fmt.Printf("Deleted %s (%s) from %q\n", args[0], entityID, deleteDB)
		return nil
	},
}

func init() {
	deleteCmd.Flags().StringVar(&deleteDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "confirm deletion (required — delete is destructive)")
	rootCmd.AddCommand(deleteCmd)
}
