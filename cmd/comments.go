package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var commentsCmd = &cobra.Command{
	Use:   "comments",
	Short: "Read comments on Fibery entities",
}

var commentsListDB string

var commentsListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List comments on an entity (oldest first, with markdown bodies)",
	Long: `List all comments on an entity. Accepts UUID, "DT-42", or "42".

Each comment is rendered as a Markdown section with author, date, and body.
The body is fetched separately per comment via the documents API.

Example:
  fibery comments list 42 --db "Development/Dev Task"
  fibery comments list DT-42 --db "Development/Dev Task"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if commentsListDB == "" {
			return fmt.Errorf("--db is required")
		}
		entityID, err := resolveEntityRef(cmd.Context(), commentsListDB, args[0])
		if err != nil {
			return err
		}

		// One query: navigate the entity's comments/comments collection, pulling
		// author, date, and document secret for each comment. Document bodies
		// are fetched in a second batch below.
		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from": commentsListDB,
					"q/select": map[string]any{
						"Comments": map[string]any{
							"q/from": "comments/comments",
							"q/select": map[string]any{
								"ID":       []any{"fibery/id"},
								"PublicID": []any{"fibery/public-id"},
								"Created":  []any{"fibery/creation-date"},
								"Author":   []any{"comment/author", "user/name"},
								"Secret":   []any{"comment/document-secret"},
							},
							"q/order-by": []any{[]any{[]any{"fibery/creation-date"}, "q/asc"}},
							"q/limit":    "q/no-limit",
						},
					},
					"q/where": []any{"=", []any{"fibery/id"}, "$eid"},
					"q/limit": 1,
				},
				"params": map[string]any{"$eid": entityID},
			},
		})
		if err != nil {
			return err
		}

		var rows []map[string]any
		if err := json.Unmarshal(result, &rows); err != nil {
			return fmt.Errorf("comments list: parse: %w", err)
		}
		if len(rows) == 0 {
			return fmt.Errorf("entity %q not found in %s", args[0], commentsListDB)
		}
		commentsAny, _ := rows[0]["Comments"].([]any)
		if len(commentsAny) == 0 {
			fmt.Fprintln(os.Stdout, "_No comments._")
			return nil
		}

		// JSON output mode: dump the raw comments array (without doc bodies).
		// Document fetches happen only for the rendered path since they are
		// extra round-trips.
		if outputFormat == "json" || (outputFormat == "" && jsonOutput) {
			rawComments, _ := json.MarshalIndent(commentsAny, "", "  ")
			fmt.Println(string(rawComments))
			return nil
		}

		for _, c := range commentsAny {
			cm, _ := c.(map[string]any)
			author := asStr(cm["Author"])
			if author == "" {
				author = "?"
			}
			created := asStr(cm["Created"])
			secret := asStr(cm["Secret"])
			body := ""
			if secret != "" {
				if got, err := cli.GetDocument(cmd.Context(), secret); err == nil {
					body = strings.TrimSpace(got)
				}
			}
			fmt.Fprintf(os.Stdout, "## %s — %s\n\n", author, created)
			// Comment id (and public id) so the comment can be targeted by
			// `fibery comment edit/delete`.
			id := asStr(cm["ID"])
			if pid := asStr(cm["PublicID"]); pid != "" {
				fmt.Fprintf(os.Stdout, "_id: %s · #%s_\n\n", id, pid)
			} else {
				fmt.Fprintf(os.Stdout, "_id: %s_\n\n", id)
			}
			if body == "" {
				fmt.Fprintln(os.Stdout, "_(empty)_")
			} else {
				fmt.Fprintln(os.Stdout, body)
			}
			fmt.Fprintln(os.Stdout)
		}
		return nil
	},
}

// resolveCommentSecret returns the document secret backing a comment, given the
// comment's fibery/id. The secret is what /api/documents reads and writes, so it
// is all that comment edit needs — the host entity is irrelevant.
func resolveCommentSecret(ctx context.Context, commentID string) (string, error) {
	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   "comments/comment",
				"q/select": map[string]any{"secret": []any{"comment/document-secret"}},
				"q/where":  []any{"=", []any{"fibery/id"}, "$id"},
				"q/limit":  1,
			},
			"params": map[string]any{"$id": commentID},
		},
	})
	if err != nil {
		return "", fmt.Errorf("query comment secret: %w", err)
	}
	var items []map[string]any
	if jErr := json.Unmarshal(raw, &items); jErr != nil || len(items) == 0 {
		return "", fmt.Errorf("comment %s not found", commentID)
	}
	secret := asStr(items[0]["secret"])
	if secret == "" {
		return "", fmt.Errorf("comment %s has no document secret", commentID)
	}
	return secret, nil
}

func init() {
	commentsListCmd.Flags().StringVar(&commentsListDB, "db", "", "database of the parent entity (e.g. \"Development/Dev Task\")")
	commentsCmd.AddCommand(commentsListCmd)
	rootCmd.AddCommand(commentsCmd)
}
