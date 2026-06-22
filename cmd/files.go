package cmd

import (
	"context"
	"encoding/json"
	"fmt"
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
	filesDB     string
	filesField  string
	filesOut    string
	filesSecret string
	filesName   string
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

	filesCmd.AddCommand(filesListCmd)
	filesCmd.AddCommand(filesDownloadCmd)
	rootCmd.AddCommand(filesCmd)
}
