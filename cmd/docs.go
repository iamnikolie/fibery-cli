package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/client"
)

// looksLikeURL reports whether s is an http(s) URL (vs a document secret or an
// entity id like "280" / "DT-42" / a UUID).
func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// docView is a space/wiki document surfaced by `docs list`.
type docView struct {
	Name     string
	PublicID string
	Space    string
}

// filterDocumentViews keeps document-type views, resolves each view's space name
// from appNames (container-app id → space name), and—when spaceFilter is set—keeps
// only documents whose space matches it (case-insensitive).
func filterDocumentViews(views []client.ViewRecord, appNames map[string]string, spaceFilter string) []docView {
	var out []docView
	want := strings.ToLower(strings.TrimSpace(spaceFilter))
	for _, v := range views {
		if v.Type != "document" {
			continue
		}
		space := appNames[v.ContainerAppID]
		if want != "" && strings.ToLower(space) != want {
			continue
		}
		out = append(out, docView{Name: v.Name, PublicID: v.PublicID, Space: space})
	}
	return out
}

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Work with space/wiki documents (left-nav pages)",
	Long: `List space/wiki documents.

Space documents are not part of the entity API; this uses Fibery's undocumented
query-views endpoint, which may change without notice.`,
}

var (
	docsListSpace string
	docsListLimit int
)

var docsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List space/wiki documents",
	Long: `List space/wiki documents across the workspace (or one space with --space).

Examples:
  fibery docs list
  fibery docs list --space Development
  fibery docs list --space Development --limit 50`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		appNames, err := loadAppNames(cmd.Context())
		if err != nil {
			return err
		}
		views, err := cli.QueryViews(cmd.Context(), nil)
		if err != nil {
			return err
		}
		docs := filterDocumentViews(views, appNames, docsListSpace)
		if docsListLimit > 0 && len(docs) > docsListLimit {
			docs = docs[:docsListLimit]
		}

		if outputFormat == "json" || jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			rows := make([]map[string]string, len(docs))
			for i, d := range docs {
				rows[i] = map[string]string{"name": d.Name, "publicId": d.PublicID, "space": d.Space, "url": docURL(d)}
			}
			return enc.Encode(rows)
		}

		fmt.Printf("| Document | ID | Space | URL |\n|----------|----|-------|-----|\n")
		for _, d := range docs {
			fmt.Printf("| %s | %s | %s | %s |\n", d.Name, d.PublicID, d.Space, docURL(d))
		}
		return nil
	},
}

// printDocumentView renders a space document in LLM-ready Markdown: a header with
// metadata (incl. the document secret) followed by the fetched content.
func printDocumentView(ctx context.Context, v *client.ViewRecord) error {
	fmt.Fprintf(os.Stdout, "# %s\n\n", strings.TrimSpace(v.Name))
	fmt.Fprintf(os.Stdout, "**Type:** document\n**Public ID:** %s\n**Document secret:** %s\n", v.PublicID, v.DocumentSecret)
	content, err := cli.GetDocument(ctx, v.DocumentSecret)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) != "" {
		fmt.Fprint(os.Stdout, "\n---\n\n")
		fmt.Fprint(os.Stdout, content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}
	return nil
}

// docURL reconstructs the Fibery URL for a space document. The slug mirrors how
// Fibery builds it: spaces in the space name become underscores, and any run of
// non-alphanumeric characters in the title becomes a single hyphen (so "/" in a
// date-style title doesn't break the URL path).
func docURL(d docView) string {
	space := strings.ReplaceAll(d.Space, " ", "_")
	return fmt.Sprintf("%s/%s/%s-%s", cfg.BaseURL(), space, slugifyTitle(d.Name), d.PublicID)
}

// slugifyTitle collapses any run of non-alphanumeric characters to a single hyphen
// and trims leading/trailing hyphens.
func slugifyTitle(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// loadAppNames returns a map of Fibery app (space) id → human space name.
func loadAppNames(ctx context.Context) (map[string]string, error) {
	result, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   "fibery/app",
				"q/select": map[string]any{"id": []any{"fibery/id"}, "name": []any{"fibery/name"}},
				"q/limit":  "q/no-limit",
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("load spaces: %w", err)
	}
	var apps []map[string]any
	if err := json.Unmarshal(result, &apps); err != nil {
		return nil, fmt.Errorf("load spaces: %w", err)
	}
	m := make(map[string]string, len(apps))
	for _, a := range apps {
		m[asStr(a["id"])] = asStr(a["name"])
	}
	return m, nil
}

func init() {
	docsListCmd.Flags().StringVar(&docsListSpace, "space", "", "filter to one space (by name)")
	docsListCmd.Flags().IntVar(&docsListLimit, "limit", 0, "max results (0 = all)")
	docsCmd.AddCommand(docsListCmd)
	rootCmd.AddCommand(docsCmd)
}
