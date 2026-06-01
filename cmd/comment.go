package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/client"
)

var commentDB string

var commentCmd = &cobra.Command{
	Use:   "comment <url-or-id> <text>",
	Short: "Add a comment to a Fibery entity",
	Long: `Add a comment. Pass a Fibery URL (no flags needed) or an entity ID with --db.
ID can be UUID, "DT-42", or "42".

By URL:
  fibery comment https://acme.fibery.io/Development/bug/Some-bug-3245 "Fixed in PR #42"

By ID:
  fibery comment 42 --db "Development/bug" "Fixed in PR #42"
  fibery comment DT-42 --db "Development/bug" "Fixed in PR #42"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idOrURL, text := args[0], args[1]

		var entityID, db string

		if strings.HasPrefix(idOrURL, "http") {
			var err error
			db, entityID, err = resolveURLToEntityID(cmd, idOrURL)
			if err != nil {
				return err
			}
		} else {
			if commentDB == "" {
				return fmt.Errorf("--db is required when not using a URL")
			}
			var err error
			entityID, err = resolveEntityRef(cmd.Context(), commentDB, idOrURL)
			if err != nil {
				return err
			}
			db = commentDB
		}

		if err := cli.AddComment(cmd.Context(), db, entityID, text); err != nil {
			return err
		}
		fmt.Println("Comment added.")
		return nil
	},
}

// resolveURLToEntityID parses a Fibery URL and returns the database name + fibery/id UUID.
func resolveURLToEntityID(cmd *cobra.Command, rawURL string) (db, entityID string, err error) {
	parsedDB, publicID, space, parseErr := resolveURL(rawURL)
	if parseErr != nil {
		return "", "", parseErr
	}

	if parsedDB == "" {
		raw, matchedDB, searchErr := searchSpaceByPublicID(cmd.Context(), space, publicID)
		if searchErr != nil {
			return "", "", fmt.Errorf("comment: entity #%s not found in space %q: %w", publicID, space, searchErr)
		}
		if raw == nil {
			return "", "", fmt.Errorf("comment: entity #%s not found in space %q", publicID, space)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return "", "", err
		}
		return matchedDB, asStr(m["ID"]), nil
	}

	result, err := cli.One(cmd.Context(), client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   parsedDB,
				"q/select": map[string]any{"UUID": []any{"fibery/id"}, "PID": []any{"fibery/public-id"}},
				"q/where":  []any{"q/contains-ignoring-case?", []any{"fibery/public-id"}, "$pid"},
				"q/limit":  5,
			},
			"params": map[string]any{"$pid": publicID},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("comment: query failed: %w", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(result, &items); err != nil || len(items) == 0 {
		return "", "", fmt.Errorf("comment: entity #%s not found in %s", publicID, parsedDB)
	}

	for _, item := range items {
		if asStr(item["PID"]) == publicID {
			return parsedDB, asStr(item["UUID"]), nil
		}
	}
	return parsedDB, asStr(items[0]["UUID"]), nil
}

func init() {
	commentCmd.Flags().StringVar(&commentDB, "db", "", "database name, required when not using a URL")
	rootCmd.AddCommand(commentCmd)
}
