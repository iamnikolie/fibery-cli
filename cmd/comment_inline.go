package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	ciDB         string
	ciField      string
	ciOn         string
	ciOccurrence int
	ciThread     string
	ciReopen     bool
)

var commentInlineCmd = &cobra.Command{
	Use:   "comment-inline",
	Short: "Read and manage inline (document) comments anchored to text in a rich-text field",
	Long: `Inline comments are anchored to a text range inside a document field, unlike entity
comments managed by 'comment'/'comments'. They live in the document body (content.comments).

  list    show inline comments on a document (with thread ids, state, anchored text, body, replies)
  add     add an inline comment anchored to a piece of text (--on "<text>")
  reply   reply within a thread (--thread <id>)
  resolve resolve/reopen a thread (--thread <id> [--reopen])
  delete  delete a thread (--thread <id>)

Target an entity by Fibery URL (no --db needed) or by id with --db. By-id forms prefer a
UUID; "DT-42"/"42" are resolved via the entity's public id. --field selects the document
field (default: the entity's primary description doc).`,
}

// timeNowMs is the epoch-ms clock used for new comment/reply timestamps.
func timeNowMs() int64 { return time.Now().UnixMilli() }

// docFieldsOf returns the full names of all rich-text (Collaboration~Documents/Document)
// fields on a database.
func docFieldsOf(schema map[string]any, db string) []string {
	var out []string
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if asStr(fm["fibery/type"]) == "Collaboration~Documents/Document" {
				out = append(out, asStr(fm["fibery/name"]))
			}
		}
	}
	return out
}

// resolveDocFieldName turns a user-supplied field (full name, short suffix, or "") into a
// full document field name on db. Empty picks the primary description doc.
func resolveDocFieldName(schema map[string]any, db, userField string) (string, error) {
	docs := docFieldsOf(schema, db)
	if userField == "" {
		// Prefer a field literally named ".../Description" (the canonical body field)
		// over the first document field, which on multi-doc entities is often a template.
		for _, name := range docs {
			if strings.HasSuffix(strings.ToLower(name), "/description") {
				return name, nil
			}
		}
		if f, ok := findDescriptionField(schema, db); ok {
			return f, nil
		}
		if len(docs) > 0 {
			return docs[0], nil
		}
		return "", fmt.Errorf("no document field on %s; pass --field", db)
	}
	for _, name := range docs {
		if name == userField || strings.EqualFold(name, userField) ||
			strings.EqualFold(name, db+"/"+userField) ||
			strings.HasSuffix(strings.ToLower(name), "/"+strings.ToLower(userField)) {
			return name, nil
		}
	}
	return "", fmt.Errorf("document field %q not found on %s; available: %s", userField, db, strings.Join(docs, ", "))
}

// resolveInlineTarget turns an entity URL/id (+ optional --db/--field) into a document secret.
func resolveInlineTarget(cmd *cobra.Command, idOrURL, db, field string) (string, error) {
	ctx := cmd.Context()
	var entityID string
	if strings.HasPrefix(idOrURL, "http") {
		var err error
		db, entityID, err = resolveURLToEntityID(cmd, idOrURL)
		if err != nil {
			return "", err
		}
	} else {
		if db == "" {
			return "", fmt.Errorf("--db is required when not passing a URL")
		}
		var err error
		entityID, err = resolveEntityRef(ctx, db, idOrURL)
		if err != nil {
			return "", err
		}
	}
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return "", err
	}
	full, err := resolveDocFieldName(schema, db, field)
	if err != nil {
		return "", err
	}
	return resolveDocSecretByID(ctx, db, entityID, full)
}

// --- list ---

var commentInlineListCmd = &cobra.Command{
	Use:   "list <url-or-id>",
	Short: "List inline comments on a document",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		secret, err := resolveInlineTarget(cmd, args[0], ciDB, ciField)
		if err != nil {
			return err
		}
		doc, err := cli.GetDocumentJSON(cmd.Context(), secret)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(doc.Content.Comments)
		return outputJSON(raw, func() error { return printInlineComments(cmd, doc) })
	},
}

func printInlineComments(cmd *cobra.Command, doc *client.DocJSON) error {
	threads := doc.Content.Comments
	if len(threads) == 0 {
		fmt.Fprintln(os.Stdout, "No inline comments.")
		return nil
	}
	var docNode map[string]any
	_ = json.Unmarshal(doc.Content.Doc, &docNode)

	idSet := map[string]bool{}
	collectAuthorIDs(threads, idSet)
	emails := resolveUserEmails(cmd.Context(), keysOf(idSet))

	for _, t := range threads {
		if m, ok := t.(map[string]any); ok {
			printOneThread(os.Stdout, m, docNode, emails, "")
		}
	}
	return nil
}

func printOneThread(w io.Writer, m, docNode map[string]any, emails map[string]string, indent string) {
	header := fmt.Sprintf("%s[%s]  state=%s  author=%s", indent, asStr(m["id"]), asStr(m["state"]), authorLabel(m, emails))
	if d, ok := m["detached"]; ok {
		header += fmt.Sprintf("  detached=%v", d)
	}
	fmt.Fprintln(w, header)
	if from, to, ok := intPair(m["from"], m["to"]); ok && docNode != nil {
		fmt.Fprintf(w, "%s  anchored: %q\n", indent, truncateRunes(pmSliceText(docNode, from, to), 120))
	}
	fmt.Fprintf(w, "%s  body: %s\n", indent, bodyText(m))
	if body, ok := m["body"].(map[string]any); ok {
		if replies, ok := body["comments"].([]any); ok && len(replies) > 0 {
			fmt.Fprintf(w, "%s  replies:\n", indent)
			for _, r := range replies {
				if rm, ok := r.(map[string]any); ok {
					printOneThread(w, rm, docNode, emails, indent+"    ")
				}
			}
		}
	}
}

// --- add ---

var commentInlineAddCmd = &cobra.Command{
	Use:   "add <url-or-id> --on \"<anchor text>\" \"<body>\"",
	Short: "Add an inline comment anchored to a piece of document text",
	Long: `Anchor a comment to the first (or --occurrence N) match of --on within the document
text. Example:
  fibery comment-inline add DT-42 --db "Development/Dev Task" \
    --on "race condition" "is this still possible after the lock fix?"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ciOn == "" {
			return fmt.Errorf("--on \"<anchor text>\" is required")
		}
		secret, err := resolveInlineTarget(cmd, args[0], ciDB, ciField)
		if err != nil {
			return err
		}
		doc, err := cli.GetDocumentJSON(ctx, secret)
		if err != nil {
			return err
		}
		var docNode map[string]any
		if err := json.Unmarshal(doc.Content.Doc, &docNode); err != nil {
			return fmt.Errorf("parse document: %w", err)
		}
		from, to, err := pmFindRange(docNode, ciOn, ciOccurrence)
		if err != nil {
			return err
		}
		userID, err := currentUserID(ctx)
		if err != nil {
			return err
		}
		bodyDoc := pmParagraphDoc(args[1], client.NewUUID())
		thread := newInlineThread(from, to, bodyDoc, userID, client.NewUUID(), timeNowMs())
		doc.Content.Comments = append(doc.Content.Comments, thread)
		if err := cli.SetDocumentJSON(ctx, secret, doc.Content); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Added inline comment [%s] anchored to %q\n",
			thread["id"], truncateRunes(pmSliceText(docNode, from, to), 80))
		return nil
	},
}

// --- reply ---

var commentInlineReplyCmd = &cobra.Command{
	Use:   "reply <url-or-id> --thread <id> \"<body>\"",
	Short: "Reply within an inline comment thread",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ciThread == "" {
			return fmt.Errorf("--thread <id> is required")
		}
		secret, err := resolveInlineTarget(cmd, args[0], ciDB, ciField)
		if err != nil {
			return err
		}
		doc, err := cli.GetDocumentJSON(ctx, secret)
		if err != nil {
			return err
		}
		userID, err := currentUserID(ctx)
		if err != nil {
			return err
		}
		reply := newReply(pmParagraphDoc(args[1], client.NewUUID()), userID, client.NewUUID(), timeNowMs())
		if err := inlineReply(doc.Content.Comments, ciThread, reply); err != nil {
			return err
		}
		if err := cli.SetDocumentJSON(ctx, secret, doc.Content); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Replied in thread [%s]\n", ciThread)
		return nil
	},
}

// --- resolve ---

var commentInlineResolveCmd = &cobra.Command{
	Use:   "resolve <url-or-id> --thread <id> [--reopen]",
	Short: "Resolve (or --reopen) an inline comment thread",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ciThread == "" {
			return fmt.Errorf("--thread <id> is required")
		}
		secret, err := resolveInlineTarget(cmd, args[0], ciDB, ciField)
		if err != nil {
			return err
		}
		doc, err := cli.GetDocumentJSON(ctx, secret)
		if err != nil {
			return err
		}
		state := "resolved"
		if ciReopen {
			state = "open"
		}
		if err := inlineSetState(doc.Content.Comments, ciThread, state); err != nil {
			return err
		}
		if err := cli.SetDocumentJSON(ctx, secret, doc.Content); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Thread [%s] -> %s\n", ciThread, state)
		return nil
	},
}

// --- delete ---

var commentInlineDeleteCmd = &cobra.Command{
	Use:   "delete <url-or-id> --thread <id>",
	Short: "Delete an inline comment thread",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ciThread == "" {
			return fmt.Errorf("--thread <id> is required")
		}
		secret, err := resolveInlineTarget(cmd, args[0], ciDB, ciField)
		if err != nil {
			return err
		}
		doc, err := cli.GetDocumentJSON(ctx, secret)
		if err != nil {
			return err
		}
		updated, err := inlineDelete(doc.Content.Comments, ciThread)
		if err != nil {
			return err
		}
		doc.Content.Comments = updated
		if err := cli.SetDocumentJSON(ctx, secret, doc.Content); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Deleted thread [%s]\n", ciThread)
		return nil
	},
}

// --- small local helpers ---

func collectAuthorIDs(threads []any, into map[string]bool) {
	for _, t := range threads {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if a, ok := m["author"].(map[string]any); ok {
			into[asStr(a["id"])] = true
		}
		if body, ok := m["body"].(map[string]any); ok {
			if replies, ok := body["comments"].([]any); ok {
				collectAuthorIDs(replies, into)
			}
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

func authorLabel(m map[string]any, emails map[string]string) string {
	a, _ := m["author"].(map[string]any)
	id := asStr(a["id"])
	if e := emails[id]; e != "" {
		return e
	}
	return id
}

func bodyText(m map[string]any) string {
	body, ok := m["body"].(map[string]any)
	if !ok {
		return ""
	}
	doc, ok := body["doc"].(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(pmDocText(doc))
}

func intPair(a, b any) (int, int, bool) {
	fa, oka := toInt(a)
	fb, okb := toInt(b)
	return fa, fb, oka && okb
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func init() {
	commentInlineCmd.PersistentFlags().StringVar(&ciDB, "db", "", "database name (when passing an id, not a URL)")
	commentInlineCmd.PersistentFlags().StringVar(&ciField, "field", "", "document field name (default: primary description)")

	commentInlineAddCmd.Flags().StringVar(&ciOn, "on", "", "anchor text: the substring to attach the comment to (required)")
	commentInlineAddCmd.Flags().IntVar(&ciOccurrence, "occurrence", 0, "which match of --on to use (1-based; required if ambiguous)")

	for _, c := range []*cobra.Command{commentInlineReplyCmd, commentInlineResolveCmd, commentInlineDeleteCmd} {
		c.Flags().StringVar(&ciThread, "thread", "", "target thread id (from 'comment-inline list')")
	}
	commentInlineResolveCmd.Flags().BoolVar(&ciReopen, "reopen", false, "reopen instead of resolve")

	commentInlineCmd.AddCommand(
		commentInlineListCmd,
		commentInlineAddCmd,
		commentInlineReplyCmd,
		commentInlineResolveCmd,
		commentInlineDeleteCmd,
	)
	rootCmd.AddCommand(commentInlineCmd)
}
