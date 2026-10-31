package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/spf13/cobra"
)

// exportSelect extends buildFullSelect for an archive: modification date, every
// to-many relation (public id + title), the comments collection and the file
// fields, so one paged query returns everything but document bodies.
func exportSelect(schema map[string]any, db string) (sel map[string]any, docKeys, fileFields []string, hasComments bool) {
	sel, docKeys = buildFullSelect(schema, db)
	sel["Modified"] = []any{"fibery/modification-date"}
	fileFields = discoverFileFields(schema, db)
	isFile := map[string]bool{}
	for _, f := range fileFields {
		isFile[f] = true
		sel["_files_"+f] = map[string]any{
			"q/from": f,
			"q/select": map[string]any{
				"secret": []any{"fibery/secret"},
				"name":   []any{"fibery/name"},
			},
			"q/limit": "q/no-limit",
		}
	}
	for _, t := range mustSlice(schema["fibery/types"]) {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			name, typ := asStr(fm["fibery/name"]), asStr(fm["fibery/type"])
			meta, _ := fm["fibery/meta"].(map[string]any)
			if meta["fibery/collection?"] != true || isFile[name] || skipFieldTypes[typ] {
				continue
			}
			switch {
			case name == "comments/comments":
				hasComments = true
				sel["_comments"] = map[string]any{
					"q/from": name,
					"q/select": map[string]any{
						"Created": []any{"fibery/creation-date"},
						"Author":  []any{"comment/author", "user/name"},
						"Secret":  []any{"comment/document-secret"},
						"Parent":  []any{"comments/parent", "fibery/id"},
						"ID":      []any{"fibery/id"},
					},
					"q/order-by": []any{[]any{[]any{"fibery/creation-date"}, "q/asc"}},
					"q/limit":    "q/no-limit",
				}
			case name == "assignments/assignees":
				// already selected by buildFullSelect
			case typ == "fibery/user":
				sel[fieldAlias(name)] = map[string]any{"q/from": name, "q/select": map[string]any{"Name": []any{"user/name"}}, "q/limit": "q/no-limit"}
			case isEnumLikeType(typ):
				sel[fieldAlias(name)] = map[string]any{"q/from": name, "q/select": map[string]any{"Name": []any{"enum/name"}}, "q/limit": "q/no-limit"}
			case strings.Contains(typ, "/") && !strings.HasPrefix(typ, "fibery/") && !strings.Contains(typ, "~"):
				s := map[string]any{"Public ID": []any{"fibery/public-id"}}
				if title := findTitleField(schema, typ); title != "" {
					s["Name"] = []any{title}
				}
				sel[fieldAlias(name)] = map[string]any{"q/from": name, "q/select": s, "q/limit": "q/no-limit"}
			}
		}
		break
	}
	return sel, docKeys, fileFields, hasComments
}

var reFileURL = regexp.MustCompile(`\(([^()\s]*/api/files/([A-Za-z0-9-]{16,})[^()\s]*)\)`)

// fileLinks returns the distinct (url, secret) pairs of Fibery-hosted files a
// Markdown body links to — the images pasted into a document or comment.
func fileLinks(md string) [][2]string {
	seen := map[string]bool{}
	var out [][2]string
	for _, m := range reFileURL.FindAllStringSubmatch(md, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, [2]string{m[1], m[2]})
		}
	}
	return out
}

// exportFileName builds a local file name from a slug and an id: "<id>-<slug>".
func exportFileName(id, title string) string {
	s := slugifyTitle(title)
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		return id
	}
	return id + "-" + s
}

// formatValue renders a selected value (scalar, sub-entity or collection) as one line.
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool, float64:
		return fmt.Sprint(x)
	case map[string]any:
		if n := asStr(x["Name"]); n != "" {
			if id := asStr(x["Public ID"]); id != "" {
				return n + " (#" + id + ")"
			}
			return n
		}
		if id := asStr(x["Public ID"]); id != "" {
			return "#" + id
		}
		var parts []string
		for _, k := range sortedKeys(x) {
			if s := formatValue(x[k]); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " · ")
	case []any:
		var parts []string
		for _, i := range x {
			if s := formatValue(i); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type exportStats struct {
	Database string   `json:"database"`
	Entities int      `json:"entities"`
	Docs     int      `json:"docs"`
	Comments int      `json:"comments"`
	Files    int      `json:"files"`
	Errors   []string `json:"errors,omitempty"`
}

// exporter writes one database or wiki into dir.
type exporter struct {
	ctx      context.Context
	noFiles  bool
	stats    *exportStats
	fileDir  string
	usedName map[string]bool
}

func (e *exporter) errf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	e.stats.Errors = append(e.stats.Errors, msg)
	fmt.Fprintln(os.Stderr, "  ! "+msg)
}

// localizeFiles downloads every Fibery file a body links to into e.fileDir and
// rewrites the links to relative paths (relative to the .md beside fileDir).
func (e *exporter) localizeFiles(md, relDir string) string {
	if e.noFiles {
		return md
	}
	for _, l := range fileLinks(md) {
		data, ct, err := cli.DownloadFile(e.ctx, l[1])
		if err != nil {
			e.errf("embedded file %s: %v", l[1], err)
			continue
		}
		ext := ""
		if exts, _ := mime.ExtensionsByType(strings.SplitN(ct, ";", 2)[0]); len(exts) > 0 {
			ext = exts[len(exts)-1]
		}
		name := dedupeName("embedded-"+l[1][:8]+ext, e.usedName)
		if err := os.MkdirAll(e.fileDir, 0o755); err != nil {
			e.errf("mkdir %s: %v", e.fileDir, err)
			return md
		}
		if err := os.WriteFile(filepath.Join(e.fileDir, name), data, 0o644); err != nil {
			e.errf("write %s: %v", name, err)
			continue
		}
		e.stats.Files++
		md = strings.ReplaceAll(md, "("+l[0]+")", "("+filepath.ToSlash(filepath.Join(relDir, name))+")")
	}
	return md
}

func exportDatabase(ctx context.Context, schema map[string]any, db, root string, noFiles, noComments bool) exportStats {
	st := exportStats{Database: db}
	dir := filepath.Join(root, sanitizeFilename(spaceOf(db)), sanitizeFilename(localName(db)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		st.Errors = append(st.Errors, err.Error())
		return st
	}
	sel, docKeys, fileFields, _ := exportSelect(schema, db)
	raw, err := cli.QueryAll(ctx, map[string]any{
		"q/from":     db,
		"q/select":   sel,
		"q/order-by": []any{[]any{[]any{"fibery/public-id"}, "q/asc"}},
	}, nil, 500)
	if err != nil {
		st.Errors = append(st.Errors, "query: "+err.Error())
		return st
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		st.Errors = append(st.Errors, "parse: "+err.Error())
		return st
	}
	st.Entities = len(rows)
	pretty, _ := json.MarshalIndent(rows, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "entities.json"), pretty, 0o644)
	fmt.Fprintf(os.Stderr, "%s: %d entities\n", db, len(rows))

	for i, r := range rows {
		pid := asStr(r["Public ID"])
		name := asStr(r["Name"])
		e := &exporter{ctx: ctx, noFiles: noFiles, stats: &st, fileDir: filepath.Join(dir, "files", pid), usedName: map[string]bool{}}
		rel := filepath.Join("files", pid)
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", name)
		fmt.Fprintf(&b, "| Field | Value |\n|---|---|\n| Database | %s |\n| Public ID | %s |\n", db, pid)
		for _, k := range sortedKeys(r) {
			if k == "Name" || k == "Public ID" || strings.HasPrefix(k, "_") {
				continue
			}
			if s := formatValue(r[k]); s != "" {
				fmt.Fprintf(&b, "| %s | %s |\n", k, strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " "))
			}
		}
		for _, dk := range docKeys {
			secret := ""
			if m, ok := r[dk].(map[string]any); ok {
				secret = asStr(m["Collaboration~Documents/secret"])
			} else {
				secret = asStr(r[dk])
			}
			if secret == "" {
				continue
			}
			md, err := cli.GetDocument(ctx, secret)
			if err != nil {
				e.errf("%s #%s %s: %v", db, pid, dk, err)
				continue
			}
			if strings.TrimSpace(md) == "" {
				continue
			}
			st.Docs++
			fmt.Fprintf(&b, "\n## %s\n\n%s\n", strings.TrimPrefix(dk, "_doc_"), strings.TrimSpace(e.localizeFiles(md, rel)))
		}
		for _, ff := range fileFields {
			files, _ := r["_files_"+ff].([]any)
			if len(files) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n## Files\n\n")
			for _, f := range files {
				fm, _ := f.(map[string]any)
				if noFiles {
					fmt.Fprintf(&b, "- %s (not downloaded)\n", asStr(fm["name"]))
					continue
				}
				if err := os.MkdirAll(e.fileDir, 0o755); err != nil {
					e.errf("mkdir: %v", err)
					continue
				}
				p, err := downloadOne(ctx, asStr(fm["secret"]), asStr(fm["name"]), e.fileDir, e.usedName)
				if err != nil {
					e.errf("%s #%s file %s: %v", db, pid, asStr(fm["name"]), err)
					continue
				}
				st.Files++
				fmt.Fprintf(&b, "- [%s](%s)\n", asStr(fm["name"]), filepath.ToSlash(filepath.Join(rel, filepath.Base(p))))
			}
		}
		if comments, _ := r["_comments"].([]any); len(comments) > 0 && !noComments {
			fmt.Fprintf(&b, "\n## Comments\n")
			for _, c := range comments {
				cm, _ := c.(map[string]any)
				body := fetchDocBody(ctx, asStr(cm["Secret"]))
				reply := ""
				if p, ok := cm["Parent"].(string); ok && p != "" {
					reply = " (reply)"
				}
				author := asStr(cm["Author"])
				if author == "" {
					author = "?"
				}
				fmt.Fprintf(&b, "\n### %s — %s%s\n\n%s\n", author, asStr(cm["Created"]), reply, e.localizeFiles(body, rel))
				st.Comments++
			}
		}
		path := filepath.Join(dir, exportFileName(pid, name)+".md")
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			e.errf("write %s: %v", path, err)
		}
		if (i+1)%25 == 0 {
			fmt.Fprintf(os.Stderr, "  %d/%d\n", i+1, len(rows))
		}
	}
	return st
}

func exportWiki(ctx context.Context, space, root string, noFiles bool) exportStats {
	st := exportStats{Database: space + " (wiki)"}
	appNames, err := loadAppNames(ctx)
	if err != nil {
		st.Errors = append(st.Errors, err.Error())
		return st
	}
	views, err := cli.QueryViews(ctx, nil)
	if err != nil {
		st.Errors = append(st.Errors, err.Error())
		return st
	}
	dir := filepath.Join(root, sanitizeFilename(space), "wiki")
	for _, v := range views {
		if v.Type != "document" || !strings.EqualFold(appNames[v.ContainerAppID], space) || v.DocumentSecret == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			st.Errors = append(st.Errors, err.Error())
			return st
		}
		md, err := cli.GetDocument(ctx, v.DocumentSecret)
		if err != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("wiki %s: %v", v.Name, err))
			continue
		}
		base := exportFileName(v.PublicID, v.Name)
		e := &exporter{ctx: ctx, noFiles: noFiles, stats: &st, fileDir: filepath.Join(dir, "files", v.PublicID), usedName: map[string]bool{}}
		md = e.localizeFiles(md, filepath.Join("files", v.PublicID))
		body := fmt.Sprintf("# %s\n\n_Space: %s · document #%s_\n\n%s\n", strings.TrimSpace(v.Name), space, v.PublicID, strings.TrimSpace(md))
		if err := os.WriteFile(filepath.Join(dir, base+".md"), []byte(body), 0o644); err != nil {
			st.Errors = append(st.Errors, err.Error())
			continue
		}
		st.Docs++
	}
	fmt.Fprintf(os.Stderr, "%s wiki: %d documents\n", space, st.Docs)
	return st
}

var (
	exportOut        string
	exportSpaces     []string
	exportNoFiles    bool
	exportNoComments bool
)

var exportCmd = &cobra.Command{
	Use:   "export [database...]",
	Short: "Archive databases (and spaces' wiki documents) to Markdown + JSON",
	Long: `Write an offline archive: per database an entities.json with every field
(to-many relations as public ids and titles), and one Markdown file per entity
with its fields, document bodies, comments (author and date) and attachments.
Images pasted into documents and comments are downloaded and relinked, so the
archive stays readable after the source is deleted. --space exports every
database of a space plus its wiki documents.

Layout: <out>/<Space>/<Database>/{entities.json, <id>-<slug>.md, files/<id>/…},
<out>/<Space>/wiki/…, <out>/manifest.json (counts and errors per database).

Examples:
  fibery export "Space/Database" --out ./archive
  fibery export --space Space --space Other --out ./archive
  fibery export "Space/Database" --out ./archive --no-files`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if exportOut == "" {
			return fmt.Errorf("--out is required")
		}
		if len(args) == 0 && len(exportSpaces) == 0 {
			return fmt.Errorf("name at least one database or --space")
		}
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		dbs := append([]string{}, args...)
		for _, s := range exportSpaces {
			found := false
			for _, d := range listDatabases(schema) {
				if spaceOf(d) == s {
					dbs = append(dbs, d)
					found = true
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "space %q has no databases — exporting its wiki only\n", s)
			}
		}
		for _, d := range dbs {
			if err := requireDatabase(schema, d); err != nil {
				return err
			}
		}
		var all []exportStats
		for _, d := range dbs {
			all = append(all, exportDatabase(ctx, schema, d, exportOut, exportNoFiles, exportNoComments))
		}
		for _, s := range exportSpaces {
			all = append(all, exportWiki(ctx, s, exportOut, exportNoFiles))
		}
		manifest, _ := json.MarshalIndent(map[string]any{"workspace": cfg.BaseURL(), "exports": all}, "", "  ")
		if err := os.WriteFile(filepath.Join(exportOut, "manifest.json"), manifest, 0o644); err != nil {
			return err
		}
		fmt.Printf("| Source | Entities | Docs | Comments | Files | Errors |\n|---|---|---|---|---|---|\n")
		failed := 0
		for _, s := range all {
			fmt.Printf("| %s | %d | %d | %d | %d | %d |\n", s.Database, s.Entities, s.Docs, s.Comments, s.Files, len(s.Errors))
			failed += len(s.Errors)
		}
		fmt.Printf("\nWritten to %s (manifest.json)\n", exportOut)
		if failed > 0 {
			return fmt.Errorf("%d errors — see manifest.json", failed)
		}
		return nil
	},
}

func init() {
	exportCmd.Flags().StringVar(&exportOut, "out", "", "output directory (required)")
	exportCmd.Flags().StringArrayVar(&exportSpaces, "space", nil, "export every database of this space plus its wiki documents (repeatable)")
	exportCmd.Flags().BoolVar(&exportNoFiles, "no-files", false, "skip attachments and embedded images")
	exportCmd.Flags().BoolVar(&exportNoComments, "no-comments", false, "skip comments")
	rootCmd.AddCommand(exportCmd)
}
