package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var commentsCmd = &cobra.Command{
	Use:   "comments",
	Short: "Read comments on Fibery entities",
}

var (
	commentsListDB    string
	commentsListLimit int
	commentsListSince string
)

var commentsListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List comments on an entity (oldest first, with markdown bodies)",
	Long: `List all comments on an entity. Accepts UUID, "DT-42", or "42".

Each comment is rendered as a Markdown section with author, date, and body.
The body is fetched separately per comment via the documents API.

By default every comment is returned. For incremental re-reads, narrow the set
so only the bodies you need are fetched:
  --limit N        return only the latest N comments (still rendered oldest-first)
  --since <when>   return only comments newer than <when> — RFC3339
                   (2026-06-20T00:00:00Z) or relative (24h, 7d, 30m)

Example:
  fibery comments list 42 --db "Development/Dev Task"
  fibery comments list DT-42 --db "Development/Dev Task"
  fibery comments list 42 --db "Development/Dev Task" --limit 3
  fibery comments list 42 --db "Development/Dev Task" --since 24h`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if commentsListDB == "" {
			return fmt.Errorf("--db is required")
		}
		if commentsListLimit < 0 {
			return fmt.Errorf("--limit must be >= 0")
		}

		// --since: parse to an absolute time and push it into the server query as
		// a q/where on the comment creation-date, so comments older than the bound
		// never cost a body fetch (and are never even transferred).
		var sinceParam string
		if commentsListSince != "" {
			since, err := parseSince(commentsListSince, time.Now())
			if err != nil {
				return err
			}
			sinceParam = since.UTC().Format(time.RFC3339)
		}

		entityID, err := resolveEntityRef(cmd.Context(), commentsListDB, args[0])
		if err != nil {
			return err
		}

		// One query: navigate the entity's comments/comments collection, pulling
		// author, date, and document secret for each comment. Document bodies
		// are fetched in a second batch below.
		commentsQuery := map[string]any{
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
		}
		params := map[string]any{"$eid": entityID}
		if sinceParam != "" {
			commentsQuery["q/where"] = []any{">", []any{"fibery/creation-date"}, "$since"}
			params["$since"] = sinceParam
		}

		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from":   commentsListDB,
					"q/select": map[string]any{"Comments": commentsQuery},
					"q/where":  []any{"=", []any{"fibery/id"}, "$eid"},
					"q/limit":  1,
				},
				"params": params,
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
		// --limit: keep only the latest N (the tail of the oldest-first list), so
		// bodies are fetched only for the comments actually rendered.
		if commentsListLimit > 0 {
			commentsAny = latestN(commentsAny, commentsListLimit)
		}
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

// latestN returns the last n elements of items, preserving their order. It is
// used to keep only the newest N comments from an oldest-first list while still
// rendering them oldest-first. n <= 0 or n >= len(items) returns items unchanged.
func latestN(items []any, n int) []any {
	if n <= 0 || n >= len(items) {
		return items
	}
	return items[len(items)-n:]
}

// reRelDays matches a bare day count like "7d" — time.ParseDuration has no day
// unit, so days are handled separately.
var reRelDays = regexp.MustCompile(`^(\d+)d$`)

// parseSince turns a --since value into an absolute time. It accepts RFC3339
// (e.g. 2026-06-20T00:00:00Z) and relative durations counted back from now
// (e.g. 24h, 7d, 30m). An unparseable value yields an error naming both forms.
func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("--since: empty value")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	d, err := parseRelativeDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since %q: expected RFC3339 like 2026-06-20T00:00:00Z or relative like 24h, 7d, 30m", s)
	}
	return now.Add(-d), nil
}

// parseRelativeDuration parses a relative duration, extending time.ParseDuration
// with a day unit ("7d" → 168h).
func parseRelativeDuration(s string) (time.Duration, error) {
	if m := reRelDays.FindStringSubmatch(s); m != nil {
		days, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func init() {
	commentsListCmd.Flags().StringVar(&commentsListDB, "db", "", "database of the parent entity (e.g. \"Development/Dev Task\")")
	commentsListCmd.Flags().IntVar(&commentsListLimit, "limit", 0, "return only the latest N comments (rendered oldest-first); 0 = all")
	commentsListCmd.Flags().StringVar(&commentsListSince, "since", "", "return only comments newer than this — RFC3339 or relative (24h, 7d, 30m)")
	commentsCmd.AddCommand(commentsListCmd)
	rootCmd.AddCommand(commentsCmd)
}
