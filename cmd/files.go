package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/langgerone/fibery-cli/internal/render"
)

var (
	filesDB       string
	filesField    string
	filesOut      string
	filesSecret   string
	filesName     string
	filesNoAttach bool
)

// fileEntry is one attached file, rendered to the user and emitted as JSON.
type fileEntry struct {
	Name        string `json:"name"`
	ContentType string `json:"content-type"`
	Size        int64  `json:"size"`
	Secret      string `json:"secret"`
	Field       string `json:"field"`
}

var filesCmd = &cobra.Command{
	Use:   "files",
	Short: "List and download file attachments on entities",
}

var filesListCmd = &cobra.Command{
	Use:   "list <entity-id>",
	Short: "List file attachments on an entity",
	Long: `List the files attached to an entity. Provide --db with the database name.

When --field is omitted, every file-type field on the database is queried.

Examples:
  fibery files list 75 --db "Development/Dev Task"
  fibery files list DT-75 --db "Development/Dev Task" --field "Files/Files"
  fibery files list 75 --db "Development/Dev Task" --format json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if filesDB == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}
		files, err := listEntityFiles(cmd.Context(), filesDB, args[0], filesField)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(files)
		if err != nil {
			return err
		}
		return outputJSON(raw, func() error {
			return render.List(os.Stdout, raw)
		})
	},
}

var filesDownloadCmd = &cobra.Command{
	Use:   "download [entity-id]",
	Short: "Download file attachments to disk",
	Long: `Download files attached to an entity into --out (default "."), preserving the
original file names (sanitized for the filesystem; name collisions are de-duped).
Each saved path is printed to stdout.

When --field is omitted, every file-type field on the database is downloaded.

A single file can be downloaded directly by its secret with --secret (and an
optional --name), without an entity ID.

Examples:
  fibery files download 75 --db "Development/Dev Task" --out /tmp/fibtest
  fibery files download DT-75 --db "Development/Dev Task" --field "Files/Files"
  fibery files download --secret c9356012-1e22-46a6-a564-ad5243249345 --name report.csv --out /tmp`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(filesOut, 0o755); err != nil {
			return fmt.Errorf("create output dir %q: %w", filesOut, err)
		}

		// Direct mode: download a single file by secret, no entity lookup.
		if filesSecret != "" {
			name := filesName
			if name == "" {
				name = filesSecret
			}
			path, err := downloadOne(cmd.Context(), filesSecret, name, filesOut, map[string]bool{})
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		}

		if len(args) == 0 {
			return fmt.Errorf("provide an entity ID, or use --secret to download a single file directly")
		}
		if filesDB == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}

		files, err := listEntityFiles(cmd.Context(), filesDB, args[0], filesField)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			fmt.Fprintln(os.Stderr, "No file attachments found.")
			return nil
		}

		used := map[string]bool{}
		var saved int
		for _, f := range files {
			if f.Secret == "" {
				continue
			}
			path, err := downloadOne(cmd.Context(), f.Secret, f.Name, filesOut, used)
			if err != nil {
				return fmt.Errorf("download %q: %w", f.Name, err)
			}
			fmt.Println(path)
			saved++
		}
		fmt.Fprintf(os.Stderr, "Saved %d file(s) to %s\n", saved, filesOut)
		return nil
	},
}

var filesUploadCmd = &cobra.Command{
	Use:   "upload <entity-id> <file>...",
	Short: "Upload local files and attach them to an entity's file field",
	Long: `Upload one or more local files and attach them to an entity's file field.

When --field is omitted the database's single file field is used; if the database
has several file fields, pass --field to choose.

With --no-attach the files are uploaded but not attached to any entity; the
secret and id of each are printed (a building block for scripting / embedding).

Examples:
  fibery files upload 75 --db "Development/Dev Task" diagram.png screenshot.png
  fibery files upload DT-75 --db "Development/Dev Task" --field "Files/Files" report.pdf
  fibery files upload --no-attach diagram.png`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// --no-attach: upload only, no entity, print secret/id/name (tab-separated).
		if filesNoAttach {
			for _, path := range args {
				fi, err := uploadLocalFile(ctx, path)
				if err != nil {
					return err
				}
				fmt.Printf("%s\t%s\t%s\n", fi.Secret, fi.ID, fi.Name)
			}
			return nil
		}

		if len(args) < 2 {
			return fmt.Errorf("provide <entity-id> and at least one file (or use --no-attach to upload without attaching)")
		}
		if filesDB == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}
		entityRef, paths := args[0], args[1:]

		field, err := resolveFileField(filesDB, filesField)
		if err != nil {
			return err
		}
		uuid, err := resolveEntityRef(ctx, filesDB, entityRef)
		if err != nil {
			return err
		}

		var items []any
		for _, path := range paths {
			fi, err := uploadLocalFile(ctx, path)
			if err != nil {
				return err
			}
			items = append(items, map[string]any{"fibery/id": fi.ID})
			fmt.Printf("uploaded %s (%s)\n", fi.Name, fi.Secret)
		}
		if err := addCollectionItems(ctx, filesDB, uuid, field, items); err != nil {
			return fmt.Errorf("attach to %s: %w", field, err)
		}
		fmt.Fprintf(os.Stderr, "Attached %d file(s) to %s on %s\n", len(items), field, entityRef)
		return nil
	},
}

var filesEmbedCmd = &cobra.Command{
	Use:   "embed <entity-id> <image>...",
	Short: "Upload images and embed them inline into a rich-text document field",
	Long: `Upload one or more images and append them inline (as Markdown image links)
to an entity's rich-text document field, e.g. its Description. The images render
inside the document body — the same way a pasted image does in the Fibery UI.

Examples:
  fibery files embed 75 --db "Development/Dev Task" --field "Development/Description" diagram.png
  fibery files embed DT-75 --db "Development/Dev Task" --field "Development/Description" a.png b.png`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if filesDB == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}
		if filesField == "" {
			return fmt.Errorf("--field is required (the rich-text/document field, e.g. \"Development/Description\")")
		}
		entityRef, paths := args[0], args[1:]

		uuid, err := resolveEntityRef(ctx, filesDB, entityRef)
		if err != nil {
			return err
		}
		secret, err := resolveDocSecretByID(ctx, filesDB, uuid, filesField)
		if err != nil {
			return err
		}
		content, err := cli.GetDocument(ctx, secret)
		if err != nil {
			return err
		}

		var b strings.Builder
		b.WriteString(content)
		for _, path := range paths {
			fi, err := uploadLocalFile(ctx, path)
			if err != nil {
				return err
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(inlineImageMarkdown(fi.Name, fi.Secret))
			fmt.Printf("embedded %s (%s)\n", fi.Name, fi.Secret)
		}
		if err := cli.SetDocument(ctx, secret, b.String()); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Embedded %d image(s) into %s on %s\n", len(paths), filesField, entityRef)
		return nil
	},
}

// resolveFileField returns the file field to use: field if given, otherwise the
// database's single file field. Errors when there are zero or several.
func resolveFileField(db, field string) (string, error) {
	if field != "" {
		return field, nil
	}
	schema, _ := cache.LoadSchema(account)
	fields := discoverFileFields(schema, db)
	switch len(fields) {
	case 0:
		return "", fmt.Errorf("no file fields found on %q — pass --field explicitly", db)
	case 1:
		return fields[0], nil
	default:
		return "", fmt.Errorf("multiple file fields on %q (%s) — pass --field to choose", db, strings.Join(fields, ", "))
	}
}

// uploadLocalFile reads a local file and uploads it, inferring the content type
// from its extension.
func uploadLocalFile(ctx context.Context, path string) (*client.FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	name := filepath.Base(path)
	return cli.UploadFile(ctx, name, detectContentType(name), data)
}

// inlineImageMarkdown renders the Markdown image link Fibery uses for inline
// attachments in rich-text documents: ![name](/api/files/<secret>).
func inlineImageMarkdown(name, secret string) string {
	return fmt.Sprintf("![%s](/api/files/%s)", markdownAltText(name), secret)
}

// markdownAltText neutralizes characters that would break the ![..](..) syntax.
func markdownAltText(name string) string {
	repl := strings.NewReplacer("\n", " ", "\r", " ", "[", " ", "]", " ")
	out := strings.TrimSpace(repl.Replace(name))
	if out == "" {
		return "image"
	}
	return out
}

// contentTypeByExt maps common extensions to MIME types deterministically, so
// uploads don't depend on the host's mime database for the usual cases.
var contentTypeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".pdf":  "application/pdf",
	".csv":  "text/csv",
	".txt":  "text/plain",
	".json": "application/json",
	".md":   "text/markdown",
}

// detectContentType returns the MIME type for a filename from its extension,
// falling back to the system mime database, then application/octet-stream.
func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ct, ok := contentTypeByExt[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// listEntityFiles resolves an entity reference and returns its attached files.
// When field is empty, all file-type fields on the DB are queried.
func listEntityFiles(ctx context.Context, db, entityRef, field string) ([]fileEntry, error) {
	schema, _ := cache.LoadSchema(account)

	var fields []string
	if field != "" {
		fields = []string{field}
	} else {
		fields = discoverFileFields(schema, db)
		if len(fields) == 0 {
			return nil, fmt.Errorf("no file fields found on %q — pass --field explicitly (run 'fibery schema show %q')", db, db)
		}
	}

	uuid, err := resolveEntityRef(ctx, db, entityRef)
	if err != nil {
		return nil, err
	}

	result, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args:    buildFilesQuery(db, fields, uuid),
	})
	if err != nil {
		return nil, err
	}
	return parseFileEntries(result, fields)
}

// downloadOne fetches one file by secret and writes it to outDir under a
// sanitized, de-duplicated name. Returns the path written.
func downloadOne(ctx context.Context, secret, name, outDir string, used map[string]bool) (string, error) {
	data, _, err := cli.DownloadFile(ctx, secret)
	if err != nil {
		return "", err
	}
	fname := dedupeName(sanitizeFilename(name), used)
	path := filepath.Join(outDir, fname)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// parseFileEntries flattens the file collections returned for an entity into a
// single list, tagging each file with the field it came from.
func parseFileEntries(raw json.RawMessage, fields []string) ([]fileEntry, error) {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse files response: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	entity := items[0]

	out := []fileEntry{}
	for _, field := range fields {
		rawField, ok := entity[field]
		if !ok {
			continue
		}
		var files []struct {
			Secret        string `json:"secret"`
			Name          string `json:"name"`
			ContentType   string `json:"content-type"`
			ContentLength int64  `json:"content-length"`
		}
		if err := json.Unmarshal(rawField, &files); err != nil {
			continue
		}
		for _, f := range files {
			out = append(out, fileEntry{
				Name:        f.Name,
				ContentType: f.ContentType,
				Size:        f.ContentLength,
				Secret:      f.Secret,
				Field:       field,
			})
		}
	}
	return out, nil
}

// discoverFileFields returns the names of all file-type collection fields on db
// (sorted), excluding the system avatar field. Empty when db is unknown.
func discoverFileFields(schema map[string]any, db string) []string {
	var out []string
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		fields, _ := tm["fibery/fields"].([]any)
		for _, f := range fields {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if asStr(fm["fibery/type"]) != "fibery/file" {
				continue
			}
			name := asStr(fm["fibery/name"])
			if name == "avatar/avatars" {
				continue
			}
			out = append(out, name)
		}
		break
	}
	sort.Strings(out)
	return out
}

// buildFilesQuery builds the FQL to read the given file fields off one entity.
// Each file field is a to-many relation to fibery/file, queried as a sub-select.
func buildFilesQuery(db string, fields []string, entityID string) map[string]any {
	sel := make(map[string]any, len(fields))
	for _, f := range fields {
		sel[f] = map[string]any{
			"q/from": f,
			"q/select": map[string]any{
				"secret":         []any{"fibery/secret"},
				"name":           []any{"fibery/name"},
				"content-type":   []any{"fibery/content-type"},
				"content-length": []any{"fibery/content-length"},
			},
			"q/limit": "q/no-limit",
		}
	}
	return map[string]any{
		"query": map[string]any{
			"q/from":   db,
			"q/select": sel,
			"q/where":  []any{"=", []any{"fibery/id"}, "$id"},
			"q/limit":  1,
		},
		"params": map[string]any{"$id": entityID},
	}
}

// sanitizeFilename converts a Fibery file name into a safe local filename: it
// strips directory components and path traversal, removes control and
// zero-width/format characters, trims surrounding whitespace, and falls back to
// "file" when nothing usable remains.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = filepath.Base(name)
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == 0 {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}

// dedupeName returns name if unused, otherwise inserts " (n)" before the
// extension until the result is unique. It records the chosen name in used.
func dedupeName(name string, used map[string]bool) string {
	if !used[name] {
		used[name] = true
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !used[cand] {
			used[cand] = true
			return cand
		}
	}
}

func init() {
	filesListCmd.Flags().StringVar(&filesDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	filesListCmd.Flags().StringVar(&filesField, "field", "", "file field name (default: all file fields on the DB)")

	filesDownloadCmd.Flags().StringVar(&filesDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	filesDownloadCmd.Flags().StringVar(&filesField, "field", "", "file field name (default: all file fields on the DB)")
	filesDownloadCmd.Flags().StringVar(&filesOut, "out", ".", "output directory")
	filesDownloadCmd.Flags().StringVar(&filesSecret, "secret", "", "download a single file by its fibery/secret (no entity ID needed)")
	filesDownloadCmd.Flags().StringVar(&filesName, "name", "", "filename to use with --secret")

	filesUploadCmd.Flags().StringVar(&filesDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	filesUploadCmd.Flags().StringVar(&filesField, "field", "", "file field name (default: the DB's only file field)")
	filesUploadCmd.Flags().BoolVar(&filesNoAttach, "no-attach", false, "upload only; print secret/id without attaching to an entity")

	filesEmbedCmd.Flags().StringVar(&filesDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	filesEmbedCmd.Flags().StringVar(&filesField, "field", "", "rich-text/document field to embed into (e.g. \"Development/Description\")")

	filesCmd.AddCommand(filesListCmd)
	filesCmd.AddCommand(filesDownloadCmd)
	filesCmd.AddCommand(filesUploadCmd)
	filesCmd.AddCommand(filesEmbedCmd)
	rootCmd.AddCommand(filesCmd)
}
