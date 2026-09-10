package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Read or write entity document content",
}

var (
	docDB        string
	docField     string
	docSecretArg bool
)

var docGetCmd = &cobra.Command{
	Use:   "get <secret|id|url>",
	Short: "Get document content as Markdown",
	Long: `Fetch a document by its secret, by a space-document URL, or by entity ID
(UUID / "DT-42" / "42") with --db and --field.

Examples:
  fibery doc get abc123secret
  fibery doc get https://acme.fibery.io/Development/My-Doc-280
  fibery doc get 42 --db "Space/Database" --field "Space/Description"
  fibery doc get 550e8400-e29b-41d4-a716-446655440000 --db "Space/Database" --field "Space/Description"
  fibery doc get 550e8400-e29b-41d4-a716-446655440000 --secret   # raw secret that looks like a UUID`,
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
	Use:   "set <secret|id|url> <markdown-text>",
	Short: "Set document content from Markdown text",
	Long: `Write Markdown content to a document by its secret, by a space-document URL, or by
entity ID (UUID / "DT-42" / "42") with --db and --field.

Examples:
  fibery doc set abc123secret "# Title\n\nHello world"
  fibery doc set https://acme.fibery.io/Development/My-Doc-280 "# Title\n\nNew content"
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

// resolveDocSecret turns the first argument into a document secret. It accepts:
//   - a Fibery space-document URL (resolved via query-views);
//   - a raw document secret (when --secret is passed, or when it doesn't look like
//     an entity reference);
//   - an entity reference (UUID / "42" / "DT-42") together with --db and --field.
func resolveDocSecret(cmd *cobra.Command, idOrSecret string) (string, error) {
	if looksLikeURL(idOrSecret) {
		return resolveDocSecretByURL(cmd.Context(), idOrSecret)
	}
	if docSecretArg {
		return idOrSecret, nil
	}
	if !looksLikeEntityRef(idOrSecret) {
		return idOrSecret, nil
	}
	if docDB == "" || docField == "" {
		return "", fmt.Errorf("--db and --field are required when passing an entity ID (or pass --secret if this is a raw document secret)")
	}
	uuid, err := resolveEntityRef(cmd.Context(), docDB, idOrSecret)
	if err != nil {
		return "", err
	}
	return resolveDocSecretByID(cmd.Context(), docDB, uuid, docField)
}

// resolveDocSecretByURL resolves a Fibery space/wiki document URL to its document
// secret via the query-views endpoint.
func resolveDocSecretByURL(ctx context.Context, rawURL string) (string, error) {
	_, publicID, _, err := resolveURL(rawURL)
	if err != nil {
		return "", err
	}
	if publicID == "" {
		return "", fmt.Errorf("no document id found in URL %q", rawURL)
	}
	v, err := cli.QueryView(ctx, publicID)
	if err != nil {
		return "", err
	}
	if v.DocumentSecret == "" {
		return "", fmt.Errorf("%q (#%s) is a %s, not a document", v.Name, v.PublicID, v.Type)
	}
	return v.DocumentSecret, nil
}

var docAppendCmd = &cobra.Command{
	Use:   "append <secret|id|url> <markdown-text>",
	Short: "Append Markdown to the end of a document",
	Long: `Append Markdown to a document's existing content (separated by a blank line).
Accepts a secret, a space-document URL, or an entity ID with --db and --field.

Examples:
  fibery doc append abc123secret "## New section\n\nMore text"
  fibery doc append https://acme.fibery.io/Development/My-Doc-280 "appended line"
  fibery doc append 42 "appended" --db "Space/Database" --field "Space/Description"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		secret, err := resolveDocSecret(cmd, args[0])
		if err != nil {
			return err
		}
		existing, err := cli.GetDocument(cmd.Context(), secret)
		if err != nil {
			return err
		}
		addition := unescapeDocContent(args[1])
		combined := addition
		if strings.TrimSpace(existing) != "" {
			combined = strings.TrimRight(existing, "\n") + "\n\n" + addition
		}
		if err := cli.SetDocument(cmd.Context(), secret, combined); err != nil {
			return err
		}
		fmt.Println("Document updated.")
		return nil
	},
}

func init() {
	docGetCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docGetCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docGetCmd.Flags().BoolVar(&docSecretArg, "secret", false, "treat the argument as a raw document secret (skip entity-id detection)")
	docSetCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docSetCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docSetCmd.Flags().BoolVar(&docSecretArg, "secret", false, "treat the argument as a raw document secret (skip entity-id detection)")
	docAppendCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docAppendCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docAppendCmd.Flags().BoolVar(&docSecretArg, "secret", false, "treat the argument as a raw document secret (skip entity-id detection)")
	docCmd.AddCommand(docGetCmd)
	docCmd.AddCommand(docSetCmd)
	docCmd.AddCommand(docAppendCmd)
	rootCmd.AddCommand(docCmd)
}
