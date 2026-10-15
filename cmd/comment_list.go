package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

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
		if err := validateDB(commentsListDB); err != nil {
			return err
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

		rows, err := collectCommentRows(cmd.Context(), commentsListDB, entityID, commentsListLimit, sinceParam)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			fmt.Fprintln(os.Stdout, "_No comments._")
			return nil
		}

		// JSON output mode: dump the comment metadata rows (without doc bodies).
		// Document fetches happen only for the rendered path since they are
		// extra round-trips.
		if outputFormat == "json" || (outputFormat == "" && jsonOutput) {
			raw, _ := json.MarshalIndent(rows, "", "  ")
			fmt.Println(string(raw))
			return nil
		}

		renderComments(cmd.Context(), os.Stdout, rows)
		return nil
	},
}

// commentRow is one comment's metadata (body fetched separately by secret).
type commentRow struct {
	ID       string `json:"id"`
	PublicID string `json:"publicId"`
	Author   string `json:"author"`
	Created  string `json:"created"`
	Secret   string `json:"secret"`
}

// fetchDocBody is the document-body fetch, indirected so tests can stub it.
var fetchDocBody = func(ctx context.Context, secret string) string {
	if secret == "" {
		return ""
	}
	got, err := cli.GetDocument(ctx, secret)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(got)
}

// collectCommentRows runs one query navigating the entity's comments collection,
// applying an optional --since lower bound (server-side, RFC3339) and --limit
// tail. Bodies are not fetched here.
func collectCommentRows(ctx context.Context, db, entityID string, limit int, since string) ([]commentRow, error) {
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
	if since != "" {
		commentsQuery["q/where"] = []any{">", []any{"fibery/creation-date"}, "$since"}
		params["$since"] = since
	}
	result, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": map[string]any{"Comments": commentsQuery},
				"q/where":  []any{"=", []any{"fibery/id"}, "$eid"},
				"q/limit":  1,
			},
			"params": params,
		},
	})
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(result, &rows); err != nil {
		return nil, fmt.Errorf("comments: parse: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	commentsAny, _ := rows[0]["Comments"].([]any)
	// --limit: keep only the latest N (tail of the oldest-first list) so bodies
	// are fetched only for the comments actually rendered.
	if limit > 0 {
		commentsAny = latestN(commentsAny, limit)
	}
	out := make([]commentRow, 0, len(commentsAny))
	for _, c := range commentsAny {
		cm, _ := c.(map[string]any)
		out = append(out, commentRow{
			ID:       asStr(cm["ID"]),
			PublicID: asStr(cm["PublicID"]),
			Author:   asStr(cm["Author"]),
			Created:  asStr(cm["Created"]),
			Secret:   asStr(cm["Secret"]),
		})
	}
	return out, nil
}

// renderComments writes each comment as a markdown section, fetching bodies by
// secret via fetchDocBody.
func renderComments(ctx context.Context, w io.Writer, rows []commentRow) {
	for _, r := range rows {
		author := r.Author
		if author == "" {
			author = "?"
		}
		fmt.Fprintf(w, "## %s — %s\n\n", author, r.Created)
		// Comment id (and public id) so the comment can be targeted by
		// `fibery comment edit/delete`.
		if r.PublicID != "" {
			fmt.Fprintf(w, "_id: %s · #%s_\n\n", r.ID, r.PublicID)
		} else {
			fmt.Fprintf(w, "_id: %s_\n\n", r.ID)
		}
		body := fetchDocBody(ctx, r.Secret)
		if body == "" {
			fmt.Fprintln(w, "_(empty)_")
		} else {
			fmt.Fprintln(w, body)
		}
		fmt.Fprintln(w)
	}
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
	// `comment list` is the canonical form; `comments list` keeps working via the
	// `comments` alias on commentCmd.
	commentCmd.AddCommand(commentsListCmd)
}
