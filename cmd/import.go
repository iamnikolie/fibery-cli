package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
)

var (
	importDB       string
	importFile     string
	importMakeEnum bool
)

// classifyImportRow splits JSON field values into three buckets:
// - scalars: string values (to be resolved via resolveFieldValue)
// - arrays: JSON arrays (collection/multi-select fields, handled post-create)
// - passthrough: numbers, bools, objects (passed to entity as-is)
//
// Keys are canonicalized against the schema when canon != nil so case-mismatched
// field names like "Development/name" route to "Development/Name".
func classifyImportRow(row map[string]json.RawMessage, canon func(string) string) (scalars map[string]string, arrays map[string][]json.RawMessage, passthrough map[string]any) {
	scalars = map[string]string{}
	arrays = map[string][]json.RawMessage{}
	passthrough = map[string]any{}
	for k, rawVal := range row {
		if canon != nil {
			k = canon(k)
		}
		var arrVal []json.RawMessage
		if json.Unmarshal(rawVal, &arrVal) == nil {
			arrays[k] = arrVal
			continue
		}
		var strVal string
		if json.Unmarshal(rawVal, &strVal) == nil {
			scalars[k] = strVal
			continue
		}
		var v any
		json.Unmarshal(rawVal, &v)
		passthrough[k] = v
	}
	return
}

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Bulk create entities from a JSON file",
	Long: `Create multiple entities from a JSON array file. Each object becomes one entity.
Enum and user fields are resolved by name (same as create).
JSON arrays in field values are treated as collection (multi-select) fields.

Limitations: document (rich-text) fields are not supported in import.
Set them after import with: fibery doc set <uuid> "content" --db "..." --field "..."

Example file (items.json):
  [
    {
      "Space/Name": "First item",
      "Space/Priority": "High",
      "Space/Tags": ["Backend", "API"]
    },
    {
      "Space/Name": "Second item",
      "Space/Priority": "Low",
      "Space/Tags": ["Frontend"]
    }
  ]

Usage:
  fibery import --db "Space/Database" --file items.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if importDB == "" {
			return fmt.Errorf("--db is required")
		}
		if importFile == "" {
			return fmt.Errorf("--file is required")
		}

		f, err := os.Open(importFile)
		if err != nil {
			return fmt.Errorf("import: open file: %w", err)
		}
		defer f.Close()

		var rows []map[string]json.RawMessage
		if err := json.NewDecoder(bufio.NewReader(f)).Decode(&rows); err != nil {
			return fmt.Errorf("import: parse JSON: %w", err)
		}

		schema, _ := cache.LoadSchema(account)
		created, failed := 0, 0

		type collEntry struct {
			field string
			items []json.RawMessage
		}

		for i, row := range rows {
			entity := map[string]any{}
			var colls []collEntry

			canon := func(k string) string { return resolveFieldName(schema, importDB, k) }
			scalars, arrays, passthroughFields := classifyImportRow(row, canon)

			opts := resolveOpts{createMissingEnum: importMakeEnum}
			maps.Copy(entity, passthroughFields)
			for k, strVal := range scalars {
				resolved, err := resolveFieldValueOpts(cmd.Context(), schema, importDB, k, strVal, opts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "row %d field %q: %v\n", i+1, k, err)
					continue
				}
				entity[k] = resolved
			}
			for k, items := range arrays {
				colls = append(colls, collEntry{field: k, items: items})
			}

			result, err := cli.One(cmd.Context(), client.Command{
				Command: "fibery.entity/create",
				Args:    map[string]any{"type": importDB, "entity": entity},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "row %d: create failed: %v\n", i+1, err)
				failed++
				continue
			}

			// Extract UUID for collection calls
			var createdID string
			var m map[string]json.RawMessage
			if json.Unmarshal(result, &m) == nil {
				if id, ok := m["fibery/id"]; ok {
					createdID = strings.Trim(string(id), `"`)
				}
			}

			// Add collection items
			if len(colls) > 0 && createdID == "" {
				fmt.Fprintf(os.Stderr, "row %d: could not extract fibery/id; collection fields skipped\n", i+1)
			}
			for _, col := range colls {
				if createdID == "" {
					continue
				}
				items := make([]any, 0, len(col.items))
				for _, rawItem := range col.items {
					var strItem string
					if json.Unmarshal(rawItem, &strItem) == nil {
						resolved, err := resolveFieldValueOpts(cmd.Context(), schema, importDB, col.field, strItem, opts)
						if err != nil {
							fmt.Fprintf(os.Stderr, "row %d collection %q item: %v\n", i+1, col.field, err)
							continue
						}
						items = append(items, resolved)
					} else {
						var v any
						json.Unmarshal(rawItem, &v)
						items = append(items, v)
					}
				}
				if len(items) > 0 {
					if err := addCollectionItems(cmd.Context(), importDB, createdID, col.field, items); err != nil {
						fmt.Fprintf(os.Stderr, "row %d collection %q: %v\n", i+1, col.field, err)
					}
				}
			}

			created++
		}

		fmt.Printf("Imported %d/%d entities to %q\n", created, len(rows), importDB)
		if failed > 0 {
			return fmt.Errorf("%d rows failed — see stderr for details", failed)
		}
		return nil
	},
}

func init() {
	importCmd.Flags().StringVar(&importDB, "db", "", "target database (e.g. \"Space/Database\")")
	importCmd.Flags().StringVar(&importFile, "file", "", "path to JSON array file")
	importCmd.Flags().BoolVar(&importMakeEnum, "create-missing-enum", false, "create absent enum values by name instead of skipping the field")
	rootCmd.AddCommand(importCmd)
}
