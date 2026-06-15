package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
)

var (
	commentDB       string
	commentMentions []string
	commentRefs     []string
	commentReplyTo  string
)

var commentCmd = &cobra.Command{
	Use:   "comment <url-or-id> [text]",
	Short: "Add a comment to a Fibery entity",
	Long: `Add a comment. Pass a Fibery URL (no flags needed) or an entity ID with --db.
ID can be UUID, "DT-42", or "42".

By URL:
  fibery comment https://acme.fibery.io/Development/bug/Some-bug-3245 "Fixed in PR #42"

By ID:
  fibery comment 42 --db "Development/bug" "Fixed in PR #42"
  fibery comment DT-42 --db "Development/bug" "Fixed in PR #42"

Tag a user (by email) — prepends a live @mention that notifies them:
  fibery comment DT-42 --db "Development/bug" "please review" --mention dev@acme.com

Reference another entity (by URL, or by ID within the same database):
  fibery comment DT-42 --db "Development/bug" "dup of" --ref DT-99
  fibery comment DT-42 --db "Development/bug" "see" --ref https://acme.fibery.io/Support_platform/Support_ticket/X-100

Reply to an existing comment (thread). --reply-to takes the parent comment's
UUID or public ID; the positional arg is still the host entity:
  fibery comment DT-42 --db "Development/bug" "agreed" --reply-to 36129

--mention and --ref are repeatable and may be combined. Body text is optional
when at least one --mention or --ref is given.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idOrURL := args[0]
		text := ""
		if len(args) == 2 {
			text = args[1]
		}

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

		tokens, err := buildCommentTokens(cmd, db)
		if err != nil {
			return err
		}

		content := prependTokens(tokens, text)
		if strings.TrimSpace(content) == "" {
			return fmt.Errorf("nothing to post: provide comment text or at least one --mention/--ref")
		}

		var parentCommentID string
		if commentReplyTo != "" {
			parentCommentID, err = resolveEntityRef(cmd.Context(), "comments/comment", commentReplyTo)
			if err != nil {
				return fmt.Errorf("resolve --reply-to %q: %w", commentReplyTo, err)
			}
		}

		if err := cli.AddComment(cmd.Context(), db, entityID, content, parentCommentID); err != nil {
			return err
		}
		fmt.Println("Comment added.")
		return nil
	},
}

// buildCommentTokens turns --mention emails and --ref targets into Fibery
// mention shorthand tokens, in the order they were supplied (mentions first,
// then refs). hostDB is the database of the entity being commented on, used to
// resolve bare --ref ids.
func buildCommentTokens(cmd *cobra.Command, hostDB string) ([]string, error) {
	if len(commentMentions) == 0 && len(commentRefs) == 0 {
		return nil, nil
	}

	schema, _ := cache.LoadSchema(account)
	var tokens []string

	if len(commentMentions) > 0 {
		userTypeID := findTypeID(schema, "fibery/user")
		if userTypeID == "" {
			return nil, fmt.Errorf("fibery/user type not found in schema — run 'fibery schema sync'")
		}
		for _, email := range commentMentions {
			u, err := resolveUserByEmail(cmd.Context(), email)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, buildMentionToken(userTypeID, asStr(u["fibery/id"])))
		}
	}

	for _, ref := range commentRefs {
		refDB, refID, err := resolveRefTarget(cmd, ref, hostDB)
		if err != nil {
			return nil, fmt.Errorf("resolve --ref %q: %w", ref, err)
		}
		typeID := findTypeID(schema, refDB)
		if typeID == "" {
			return nil, fmt.Errorf("type %q not found in schema — run 'fibery schema sync'", refDB)
		}
		tokens = append(tokens, buildMentionToken(typeID, refID))
	}

	return tokens, nil
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
	commentCmd.Flags().StringArrayVar(&commentMentions, "mention", nil, "email of a user to @mention (repeatable); prepended to the comment and notifies them")
	commentCmd.Flags().StringArrayVar(&commentRefs, "ref", nil, "entity to reference: a Fibery URL, or an ID/\"DT-42\" within the host database (repeatable)")
	commentCmd.Flags().StringVar(&commentReplyTo, "reply-to", "", "parent comment UUID or public ID — posts this comment as a threaded reply")
	rootCmd.AddCommand(commentCmd)
}
