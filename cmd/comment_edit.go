package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var commentEditCmd = &cobra.Command{
	Use:   "edit <comment-id> <text>",
	Short: "Replace a comment's body",
	Long: `Edit an existing comment, replacing its body. <comment-id> is the comment's
UUID or public id (both are shown by 'fibery comments list'). The host entity is
inferred automatically — no --db needed.

Text interprets \n, \t, \r, \\ escapes, same as 'fibery comment'.

Examples:
  fibery comment edit 36129 "Updated text"
  fibery comment edit 7e2c… "Line one\nLine two"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		commentID, err := resolveEntityRef(cmd.Context(), "comments/comment", args[0])
		if err != nil {
			return fmt.Errorf("resolve comment %q: %w", args[0], err)
		}
		secret, err := resolveCommentSecret(cmd.Context(), commentID)
		if err != nil {
			return err
		}
		if err := cli.SetDocument(cmd.Context(), secret, unescapeDocContent(args[1])); err != nil {
			return fmt.Errorf("edit comment: %w", err)
		}
		fmt.Println("Comment updated.")
		return nil
	},
}

func init() {
	commentCmd.AddCommand(commentEditCmd)
}
