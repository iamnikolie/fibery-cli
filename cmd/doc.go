package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iamnikolie/fibery-cli/internal/client"
)

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Read or write entity document content",
}

var (
	docDB        string
	docField     string
	docSecretArg bool
	docJSONDoc   bool

	docFile              string
	docSetJSONFile       string
	docIfUnmodifiedSince string
)

var docGetCmd = &cobra.Command{
	Use:   "get <secret|id|url>",
	Short: "Get document content as Markdown",
	Long: `Fetch a document by its secret, by a space-document URL, or by entity ID
(UUID / "DT-42" / "42") with --db and --field.

Callouts come back in Fibery's native form, which "doc set" accepts unchanged:
  > [//]: # (callout;icon-type=icon;icon=x;color=#d40915)
  > text

--json-doc prints the ProseMirror envelope {secret, content:{doc, comments},
modificationDate} instead: the form for nodes Markdown cannot carry, and the
modificationDate for "doc set --if-unmodified-since".

Examples:
  fibery doc get abc123secret
  fibery doc get https://acme.fibery.io/Development/My-Doc-280
  fibery doc get 42 --db "Space/Database" --field "Space/Description"
  fibery doc get 550e8400-e29b-41d4-a716-446655440000 --db "Space/Database" --field "Space/Description"
  fibery doc get 550e8400-e29b-41d4-a716-446655440000 --secret   # raw secret that looks like a UUID
  fibery doc get 42 --db "Space/Database" --field "Space/Description" --json-doc`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		secret, err := resolveDocSecret(cmd, args[0])
		if err != nil {
			return err
		}
		if docJSONDoc {
			d, err := cli.GetDocumentJSON(cmd.Context(), secret)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(d)
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
	Use:   "set <secret|id|url> [markdown-text]",
	Short: "Set document content from Markdown text, a file, or ProseMirror JSON",
	Long: `Replace a document's content. The document is addressed by its secret, by a
space-document URL, or by entity ID (UUID / "DT-42" / "42") with --db and --field.

The body comes from exactly one of: the positional text (\n, \t, \r escapes are
interpreted), --file (read verbatim, "-" = stdin), or --json-doc (ProseMirror JSON).

Callouts: "> [!NOTE]" / "> [!CAUTION]" blockquotes become Fibery callouts; the
native form "> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)" passes through.

--if-unmodified-since refuses the write when the document's modificationDate is
later than the given one (take it from "doc get --json-doc").

--json-doc without a "comments" array keeps the document's inline comments, but
their anchors (ProseMirror from/to positions) are not remapped: change "doc" in
place, or pass "comments" yourself. The check and the
write are two requests, so it narrows the race with a UI edit rather than closing it.

Examples:
  fibery doc set abc123secret "# Title\n\nHello world"
  fibery doc set https://acme.fibery.io/Development/My-Doc-280 --file body.md
  fibery doc set 42 --db "Space/Database" --field "Space/Description" --file body.md
  fibery doc set 42 --db "Space/Database" --field "Space/Description" --json-doc doc.json
  fibery doc set 42 --db "Space/Database" --field "Space/Description" --file body.md \
    --if-unmodified-since 2026-10-14T09:12:44.123Z`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.HasPrefix(docSetJSONFile, "-") && docSetJSONFile != "-" {
			return fmt.Errorf("--json-doc takes a file path (or \"-\" for stdin), got %q", docSetJSONFile)
		}
		if docSetJSONFile != "" && (len(args) == 2 || docFile != "") {
			return fmt.Errorf("--json-doc replaces the Markdown body: pass it without text or --file")
		}
		var body string
		if docSetJSONFile == "" {
			var err error
			if body, err = docBodyFromArgs(args, docFile); err != nil {
				return err
			}
		}
		secret, err := resolveDocSecret(cmd, args[0])
		if err != nil {
			return err
		}
		if err := checkDocUnmodified(cmd.Context(), secret, docIfUnmodifiedSince); err != nil {
			return err
		}
		if docSetJSONFile != "" {
			content, err := readDocJSONFile(cmd.Context(), secret, docSetJSONFile)
			if err != nil {
				return err
			}
			if err := cli.SetDocumentJSON(cmd.Context(), secret, content); err != nil {
				return err
			}
		} else if err := cli.SetDocument(cmd.Context(), secret, body); err != nil {
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
	Use:   "append <secret|id|url> [markdown-text]",
	Short: "Append Markdown to the end of a document",
	Long: `Append Markdown to the end of a document. Accepts a secret, a space-document URL,
or an entity ID with --db and --field.

The existing content is kept as is — inline comments, embedded views, mentions and
anything else Markdown cannot express survive. Only the appended part goes through
Markdown, converted by Fibery itself.

The addition is the positional text (escapes interpreted) or --file (verbatim,
"-" = stdin). Callouts and --if-unmodified-since work as in "doc set".

Examples:
  fibery doc append abc123secret "## New section\n\nMore text"
  fibery doc append https://acme.fibery.io/Development/My-Doc-280 "appended line"
  fibery doc append 42 --db "Space/Database" --field "Space/Description" --file section.md`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		addition, err := docBodyFromArgs(args, docFile)
		if err != nil {
			return err
		}
		secret, err := resolveDocSecret(cmd, args[0])
		if err != nil {
			return err
		}
		if err := checkDocUnmodified(cmd.Context(), secret, docIfUnmodifiedSince); err != nil {
			return err
		}
		nodes, err := cli.MarkdownToDocJSON(cmd.Context(), addition)
		if err != nil {
			return err
		}
		doc, err := cli.GetDocumentJSON(cmd.Context(), secret)
		if err != nil {
			return err
		}
		if err := appendDocNodes(&doc.Content, nodes); err != nil {
			return err
		}
		if err := cli.SetDocumentJSON(cmd.Context(), secret, doc.Content); err != nil {
			return err
		}
		fmt.Println("Document updated.")
		return nil
	},
}

// docBodyFromArgs returns the Markdown body from the positional argument (escapes
// interpreted) or from --file (verbatim; "-" reads stdin). Exactly one is required.
func docBodyFromArgs(args []string, file string) (string, error) {
	switch {
	case file != "" && len(args) == 2:
		return "", fmt.Errorf("pass the body as text or with --file, not both")
	case file == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("--file -: %w", err)
		}
		return string(b), nil
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("--file: %w", err)
		}
		return string(b), nil
	case len(args) == 2:
		return unescapeDocContent(args[1]), nil
	default:
		return "", fmt.Errorf("missing body: pass the Markdown text or --file <path>")
	}
}

// checkDocUnmodified fails when the document changed after since (an RFC 3339
// modificationDate as printed by "doc get --json-doc"). Empty since skips the check.
func checkDocUnmodified(ctx context.Context, secret, since string) error {
	if since == "" {
		return nil
	}
	sinceT, err := time.Parse(time.RFC3339Nano, since)
	if err != nil {
		return fmt.Errorf("--if-unmodified-since %q: want an RFC 3339 time such as the modificationDate from 'doc get --json-doc'", since)
	}
	d, err := cli.GetDocumentJSON(ctx, secret)
	if err != nil {
		return err
	}
	if docModifiedAfter(d.ModificationDate, sinceT) {
		return fmt.Errorf("document was modified at %s, after %s — not written; re-read it with 'doc get' and apply the change again", d.ModificationDate, since)
	}
	return nil
}

// docModifiedAfter reports whether modified is later than since. An unparseable
// modified date counts as modified, so the guard fails closed.
func docModifiedAfter(modified string, since time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, modified)
	if err != nil {
		return true
	}
	return t.After(since)
}

// readDocJSONFile reads ProseMirror content for "doc set --json-doc": either the
// full envelope printed by "doc get --json-doc" or a bare {doc, comments} object.
// When the file carries no comments array the document's current inline comments
// are kept, so a doc-only file does not wipe them. Their from/to anchors are not
// remapped to the new doc.
func readDocJSONFile(ctx context.Context, secret, path string) (client.DocContent, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return client.DocContent{}, fmt.Errorf("--json-doc: %w", err)
	}
	content, hasComments, err := parseDocJSON(raw)
	if err != nil {
		return client.DocContent{}, err
	}
	if !hasComments {
		cur, err := cli.GetDocumentJSON(ctx, secret)
		if err != nil {
			return client.DocContent{}, err
		}
		content.Comments = cur.Content.Comments
	}
	return content, nil
}

// parseDocJSON decodes a --json-doc payload and reports whether it carried a
// comments array.
func parseDocJSON(raw []byte) (client.DocContent, bool, error) {
	var probe struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return client.DocContent{}, false, fmt.Errorf("--json-doc: %w", err)
	}
	body := raw
	if strings.HasPrefix(strings.TrimSpace(string(probe.Content)), "{") {
		body = probe.Content
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return client.DocContent{}, false, fmt.Errorf("--json-doc: %w", err)
	}
	doc := fields["doc"]
	if len(doc) == 0 || string(doc) == "null" {
		return client.DocContent{}, false, fmt.Errorf("--json-doc: no \"doc\" object (want {doc, comments} or the 'doc get --json-doc' envelope)")
	}
	content := client.DocContent{Doc: doc, Comments: []any{}}
	rawComments, ok := fields["comments"]
	hasComments := ok && string(rawComments) != "null"
	if hasComments {
		if err := json.Unmarshal(rawComments, &content.Comments); err != nil {
			return client.DocContent{}, false, fmt.Errorf("--json-doc: comments: %w", err)
		}
	}
	return content, hasComments, nil
}

func init() {
	docGetCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docGetCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docGetCmd.Flags().BoolVar(&docSecretArg, "secret", false, "treat the argument as a raw document secret (skip entity-id detection)")
	docGetCmd.Flags().BoolVar(&docJSONDoc, "json-doc", false, "print the ProseMirror JSON envelope {secret, content:{doc, comments}, modificationDate} instead of Markdown")
	docSetCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docSetCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docSetCmd.Flags().BoolVar(&docSecretArg, "secret", false, "treat the argument as a raw document secret (skip entity-id detection)")
	docAppendCmd.Flags().StringVar(&docDB, "db", "", "database name (required when passing fibery/id)")
	docAppendCmd.Flags().StringVar(&docField, "field", "", "document field name (required when passing fibery/id)")
	docAppendCmd.Flags().BoolVar(&docSecretArg, "secret", false, "treat the argument as a raw document secret (skip entity-id detection)")
	docSetCmd.Flags().StringVar(&docFile, "file", "", "read the Markdown body from a file, verbatim (\"-\" = stdin)")
	docSetCmd.Flags().StringVar(&docSetJSONFile, "json-doc", "", "write ProseMirror JSON from a file: the 'doc get --json-doc' envelope or {doc, comments} (\"-\" = stdin)")
	docSetCmd.Flags().StringVar(&docIfUnmodifiedSince, "if-unmodified-since", "", "refuse the write if the document's modificationDate is later than this RFC 3339 time")
	docAppendCmd.Flags().StringVar(&docFile, "file", "", "read the Markdown to append from a file, verbatim (\"-\" = stdin)")
	docAppendCmd.Flags().StringVar(&docIfUnmodifiedSince, "if-unmodified-since", "", "refuse the write if the document's modificationDate is later than this RFC 3339 time")
	docCmd.AddCommand(docGetCmd)
	docCmd.AddCommand(docSetCmd)
	docCmd.AddCommand(docAppendCmd)
	rootCmd.AddCommand(docCmd)
}
