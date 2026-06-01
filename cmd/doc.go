package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Read or write entity document content",
}

var (
	docDB    string
	docField string
)

var docGetCmd = &cobra.Command{
	Use:   "get <secret-or-id>",
	Short: "Get document content as Markdown",
	Long: `Fetch a document by its secret, or by entity ID (UUID / "DT-42" / "42") with --db and --field.

Examples:
  fibery doc get abc123secret
  fibery doc get 42 --db "Space/Database" --field "Space/Description"
  fibery doc get 550e8400-e29b-41d4-a716-446655440000 --db "Space/Database" --field "Space/Description"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		secret, err := resolveDocSecret(cmd, args[0])
		if err != nil {
			return err
		}
		content, err := cli.GetDocument(cmd.Context(), secret)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stdout, content)
		return nil
	},
}

var docSetCmd = &cobra.Command{
	Use:   "set <secret-or-id> <markdown-text>",
	Short: "Set document content from Markdown text",
	Long: `Write Markdown content to a document by its secret, or by entity ID (UUID / "DT-42" / "42") with --db and --field.

Examples:
  fibery doc set abc123secret "# Title\n\nHello world"
  fibery doc set 42 "# Title\n\nContent" --db "Space/Database" --field "Space/Description"
  fibery doc set 550e8400-e29b-41d4-a716-446655440000 "# Title\n\nContent" --db "Space/Database" --field "Space/Description"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		secret, err := resolveDocSecret(cmd, args[0])
		if err != nil {
			return err
		}
		if err := cli.SetDocument(cmd.Context(), secret, unescapeDocContent(args[1])); err != nil {
			return err
		}
		fmt.Println("Document updated.")
		return nil
	},
}

// resolveDocSecret returns the secret unchanged when idOrSecret looks like an
// opaque document secret, or resolves an entity reference (UUID / "42" / "DT-42")
// to a document secret using --db and --field.
func resolveDocSecret(cmd *cobra.Command, idOrSecret string) (string, error) {
	if !looksLikeEntityRef(idOrSecret) {
		return idOrSecret, nil
	}
	if docDB == "" || docField == "" {
		return "", fmt.Errorf("--db and --field are required when passing an entity ID")
	}
	uuid, err := resolveEntityRef(cmd.Context(), docDB, idOrSecret)
	if err != nil {
		return "", err
	}
	return resolveDocSecretByID(cmd.Context(), docDB, uuid, docField)
}

func init() {
	docGetCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docGetCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docSetCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docSetCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docCmd.AddCommand(docGetCmd)
	docCmd.AddCommand(docSetCmd)
	rootCmd.AddCommand(docCmd)
}
