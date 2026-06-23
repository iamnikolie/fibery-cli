package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	updateDB          string
	updateDocFields   []string
	updateDocFiles    []string
	updateSkipInvalid bool
	updateMakeEnum    bool
)

var updateCmd = &cobra.Command{
	Use:   "update <id> [field=value...]",
	Short: "Update fields on a Fibery entity",
	Long: `Update entity fields. Requires --db. Accepts UUID, "DT-42", or "42".
Enum and user fields are resolved by name automatically.
Repeat a field name to append multiple values to a collection (multi-select) field.
Collection field updates append — they do not replace existing values.

Examples:
  fibery update 42 --db "Space/Database" \
    "Space/Name=Updated title" "Space/Priority=High"

  fibery update DT-42 --db "Space/Database" "Space/Priority=Critical"

  # Append two values to a multi-select field
  fibery update <id> --db "Space/Database" \
    "Space/Tags=Backend" "Space/Tags=API"

  # Update a rich-text document field inline
  fibery update <id> --db "Space/Database" \
    --doc "Space/Description=# New content\n\nParagraph here"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if updateDB == "" {
			return fmt.Errorf("--db is required")
		}
		entityID, err := resolveEntityRef(cmd.Context(), updateDB, args[0])
		if err != nil {
			return err
		}
		schema, _ := cache.LoadSchema(account)

		// Collect field values, grouping repeated keys. Field names are canonicalized
		// against the schema so "Development/name" auto-corrects to "Development/Name".
		fieldMap := map[string][]string{}
		fieldOrder := []string{}
		for _, pair := range args[1:] {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid field=value pair: %q", pair)
			}
			k = resolveFieldName(schema, updateDB, k)
			if _, exists := fieldMap[k]; !exists {
				fieldOrder = append(fieldOrder, k)
			}
			fieldMap[k] = append(fieldMap[k], v)
		}

		// Separate scalar fields from collection fields
		opts := resolveOpts{createMissingEnum: updateMakeEnum}
		entity := map[string]any{"fibery/id": entityID}
		type collField struct {
			name string
			vals []string
		}
		var collections []collField
		for _, k := range fieldOrder {
			if verr := validateField(schema, updateDB, k); verr != nil {
				if updateSkipInvalid {
					fmt.Fprintf(os.Stderr, "skipping: %v\n", verr)
					continue
				}
				return verr
			}
			vals := fieldMap[k]
			if len(vals) > 1 || isCollectionField(schema, updateDB, k) {
				collections = append(collections, collField{k, vals})
			} else {
				resolved, err := resolveFieldValueOpts(cmd.Context(), schema, updateDB, k, vals[0], opts)
				if err != nil {
					if updateSkipInvalid {
						fmt.Fprintf(os.Stderr, "skipping field %q: %v\n", k, err)
						continue
					}
					return fmt.Errorf("field %q: %w", k, err)
				}
				entity[k] = resolved
			}
		}

		// Update scalar fields (only if there are fields beyond fibery/id)
		if len(entity) > 1 {
			if _, err := cli.One(cmd.Context(), client.Command{
				Command: "fibery.entity/update",
				Args:    map[string]any{"type": updateDB, "entity": entity},
			}); err != nil {
				return err
			}
		}

		// Append collection items
		for _, col := range collections {
			items := make([]any, 0, len(col.vals))
			for _, v := range col.vals {
				item, err := resolveFieldValueOpts(cmd.Context(), schema, updateDB, col.name, v, opts)
				if err != nil {
					if updateSkipInvalid {
						fmt.Fprintf(os.Stderr, "skipping collection %q value %q: %v\n", col.name, v, err)
						continue
					}
					return fmt.Errorf("collection field %q value %q: %w", col.name, v, err)
				}
				items = append(items, item)
			}
			if len(items) == 0 {
				continue
			}
			if err := addCollectionItems(cmd.Context(), updateDB, entityID, col.name, items); err != nil {
				return fmt.Errorf("add-collection-items %q: %w", col.name, err)
			}
		}

		// Set document fields (inline --doc and file-backed --doc-file)
		docs, derr := collectDocFields(schema, updateDB, updateDocFields, updateDocFiles)
		if derr != nil {
			return derr
		}
		for _, d := range docs {
			secret, err := resolveDocSecretByID(cmd.Context(), updateDB, entityID, d.field)
			if err != nil {
				return fmt.Errorf("doc %q: %w", d.field, err)
			}
			if err := cli.SetDocument(cmd.Context(), secret, d.content); err != nil {
				return fmt.Errorf("doc %q: set content: %w", d.field, err)
			}
		}

		// Echo what was updated
		var parts []string
		parts = append(parts, fieldOrder...)
		for _, d := range docs {
			parts = append(parts, d.field+" (doc)")
		}
		if len(parts) > 0 {
			fmt.Printf("Updated %d field(s): %s\n", len(parts), strings.Join(parts, ", "))
		} else {
			fmt.Println("Updated.")
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVar(&updateDB, "db", "", "database name (e.g. \"Space/Database\")")
	updateCmd.Flags().StringArrayVar(&updateDocFields, "doc", nil, `set document field: --doc "Space/Description=# Heading\n\ncontent"`)
	updateCmd.Flags().StringArrayVar(&updateDocFiles, "doc-file", nil, `set document field from a file: --doc-file "Space/Description=path/to/body.md"`)
	updateCmd.Flags().BoolVar(&updateSkipInvalid, "skip-invalid", false, "skip fields/values that don't resolve instead of failing the whole update")
	updateCmd.Flags().BoolVar(&updateMakeEnum, "create-missing-enum", false, "create absent enum values by name instead of failing")
	rootCmd.AddCommand(updateCmd)
}
