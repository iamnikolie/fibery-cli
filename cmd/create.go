package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
)

var (
	createDocFields []string
	createIDOnly    bool
)

var createCmd = &cobra.Command{
	Use:   "create <database> [field=value...]",
	Short: "Create a new Fibery entity",
	Long: `Create a new entity. Pass field=value pairs as arguments.
Enum and user fields are resolved by name automatically.
Repeat a field name to append multiple values to a collection (multi-select) field.

Examples:
  fibery create "Space/Database" "Space/Name=My item" "Space/Priority=High"

  # Multi-select: repeat the field
  fibery create "Space/Database" "Space/Name=My item" "Space/Tags=Backend" "Space/Tags=API"

  # Inline document
  fibery create "Space/Database" "Space/Name=My item" \
    --doc "Space/Description=# Heading\n\ncontent"

  # Return only the UUID (for scripting)
  ID=$(fibery create "Space/Database" "Space/Name=My item" --id-only)`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := args[0]
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
			k = resolveFieldName(schema, db, k)
			if _, exists := fieldMap[k]; !exists {
				fieldOrder = append(fieldOrder, k)
			}
			fieldMap[k] = append(fieldMap[k], v)
		}

		// Separate scalar fields from collection fields
		entity := map[string]any{}
		type collField struct {
			name string
			vals []string
		}
		var collections []collField
		for _, k := range fieldOrder {
			vals := fieldMap[k]
			if len(vals) > 1 || isCollectionField(schema, db, k) {
				collections = append(collections, collField{k, vals})
			} else {
				resolved, err := resolveFieldValue(cmd.Context(), schema, db, k, vals[0])
				if err != nil {
					return fmt.Errorf("field %q: %w", k, err)
				}
				entity[k] = resolved
			}
		}

		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/create",
			Args:    map[string]any{"type": db, "entity": entity},
		})
		if err != nil {
			return err
		}

		// Extract created entity UUID from result
		var createdID string
		var resultMap map[string]json.RawMessage
		if json.Unmarshal(result, &resultMap) == nil {
			if id, ok := resultMap["fibery/id"]; ok {
				createdID = strings.Trim(string(id), `"`)
			}
		}

		// If we have collection or doc fields, we need the entity UUID
		if createdID == "" && (len(collections) > 0 || len(createDocFields) > 0) {
			return fmt.Errorf("could not get fibery/id from created entity")
		}

		// Add collection items via post-create API calls
		for _, col := range collections {
			items := make([]any, 0, len(col.vals))
			for _, v := range col.vals {
				item, err := resolveFieldValue(cmd.Context(), schema, db, col.name, v)
				if err != nil {
					return fmt.Errorf("collection field %q value %q: %w", col.name, v, err)
				}
				items = append(items, item)
			}
			if err := addCollectionItems(cmd.Context(), db, createdID, col.name, items); err != nil {
				return fmt.Errorf("add-collection-items %q: %w", col.name, err)
			}
		}

		// Set inline document fields
		for _, pair := range createDocFields {
			field, content, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("--doc: invalid Field=content pair: %q", pair)
			}
			field = resolveFieldName(schema, db, field)
			secret, err := resolveDocSecretByID(cmd.Context(), db, createdID, field)
			if err != nil {
				return fmt.Errorf("--doc %q: %w", field, err)
			}
			if err := cli.SetDocument(cmd.Context(), secret, unescapeDocContent(content)); err != nil {
				return fmt.Errorf("--doc %q: set content: %w", field, err)
			}
		}

		if createIDOnly {
			fmt.Println(createdID)
			return nil
		}
		// --json / --format json: outputJSON returns raw result (full entity from Fibery)
		return outputJSON(result, func() error {
			fmt.Fprintf(os.Stdout, "Created: %s\n", createdID)
			return nil
		})
	},
}

func init() {
	createCmd.Flags().StringArrayVar(&createDocFields, "doc", nil, `set document field inline: --doc "Space/Description=# Heading\n\ncontent"`)
	createCmd.Flags().BoolVar(&createIDOnly, "id-only", false, "output only the fibery/id UUID (for scripting)")
	rootCmd.AddCommand(createCmd)
}
