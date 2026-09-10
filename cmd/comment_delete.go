package cmd

import (
	"fmt"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var commentDeleteYes bool

var commentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete a comment (requires --yes)",
	Long: `Delete a comment by its UUID or public id (both are shown by
'fibery comments list'). The host entity is inferred automatically — no --db
needed. Requires --yes because deletion is irreversible.

Example:
  fibery comment delete 36129 --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !commentDeleteYes {
			return fmt.Errorf("delete is destructive — add --yes to confirm (comment %q)", args[0])
		}
		commentID, err := resolveEntityRef(cmd.Context(), "comments/comment", args[0])
		if err != nil {
			return fmt.Errorf("resolve comment %q: %w", args[0], err)
		}
		if _, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/delete",
			Args: map[string]any{
				"type":   "comments/comment",
				"entity": map[string]any{"fibery/id": commentID},
			},
		}); err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		fmt.Printf("Deleted comment %s.\n", args[0])
		return nil
	},
}

func init() {
	commentDeleteCmd.Flags().BoolVar(&commentDeleteYes, "yes", false, "confirm deletion (required — delete is destructive)")
	commentCmd.AddCommand(commentDeleteCmd)
}
