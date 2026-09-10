package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	createDocFields   []string
	createDocFiles    []string
	createIDOnly      bool
	createPublicID    bool
	createSkipInvalid bool
	createMakeEnum    bool
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

  # Document from a file (no \n escaping needed — better for long markdown)
  fibery create "Space/Database" "Space/Name=My item" \
    --doc-file "Space/Description=./body.md"

  # Return only the UUID, or only the public id (for scripting)
  ID=$(fibery create "Space/Database" "Space/Name=My item" --id-only)
  PID=$(fibery create "Space/Database" "Space/Name=My item" --public-id)`,
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
		opts := resolveOpts{createMissingEnum: createMakeEnum}
		entity := map[string]any{}
		type collField struct {
			name string
			vals []string
		}
		var collections []collField
		for _, k := range fieldOrder {
			if verr := validateField(schema, db, k); verr != nil {
				if createSkipInvalid {
					fmt.Fprintf(os.Stderr, "skipping: %v\n", verr)
					continue
				}
				return verr
			}
			vals := fieldMap[k]
			if len(vals) > 1 || isCollectionField(schema, db, k) {
				collections = append(collections, collField{k, vals})
			} else {
				resolved, err := resolveFieldValueOpts(cmd.Context(), schema, db, k, vals[0], opts)
				if err != nil {
					if createSkipInvalid {
						fmt.Fprintf(os.Stderr, "skipping field %q: %v\n", k, err)
						continue
					}
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

		// Resolve document fields up front (inline --doc and --doc-file) so a bad
		// path fails before we touch the API for collections.
		docs, derr := collectDocFields(schema, db, createDocFields, createDocFiles)
		if derr != nil {
			return derr
		}

		// If we have collection or doc fields, we need the entity UUID
		if createdID == "" && (len(collections) > 0 || len(docs) > 0) {
			return fmt.Errorf("could not get fibery/id from created entity")
		}

		// Add collection items via post-create API calls
		for _, col := range collections {
			items := make([]any, 0, len(col.vals))
			for _, v := range col.vals {
				item, err := resolveFieldValueOpts(cmd.Context(), schema, db, col.name, v, opts)
				if err != nil {
					if createSkipInvalid {
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
			if err := addCollectionItems(cmd.Context(), db, createdID, col.name, items); err != nil {
				return fmt.Errorf("add-collection-items %q: %w", col.name, err)
			}
		}

		// Set document fields (inline --doc and file-backed --doc-file)
		for _, d := range docs {
			secret, err := resolveDocSecretByID(cmd.Context(), db, createdID, d.field)
			if err != nil {
				return fmt.Errorf("doc %q: %w", d.field, err)
			}
			if err := cli.SetDocument(cmd.Context(), secret, d.content); err != nil {
				return fmt.Errorf("doc %q: set content: %w", d.field, err)
			}
		}

		if createIDOnly {
			fmt.Println(createdID)
			return nil
		}

		// Fetch the public id + title so we can echo a DT-style id and a canonical
		// URL without the caller needing a second `fibery get`. Best-effort: the
		// entity already exists, so a lookup failure must not fail the create.
		publicID, name, _, identErr := fetchIdentity(cmd.Context(), db, createdID)
		entityURL := ""
		if publicID != "" {
			entityURL = buildEntityURL(cfg.BaseURL(), db, publicID, name)
		}

		if createPublicID {
			if publicID == "" {
				return fmt.Errorf("created %s but could not fetch public id: %v", createdID, identErr)
			}
			fmt.Println(publicID)
			return nil
		}

		// Augment the raw create result with public id + url so --json/--format
		// carry everything a script needs in one shot.
		augmented := result
		var out map[string]any
		if json.Unmarshal(result, &out) == nil {
			out["fibery/id"] = createdID
			if publicID != "" {
				out["fibery/public-id"] = publicID
				out["url"] = entityURL
			}
			if b, mErr := json.Marshal(out); mErr == nil {
				augmented = b
			}
		}
		return outputJSON(augmented, func() error {
			fmt.Fprintf(os.Stdout, "Created %s\n", createdID)
			if publicID != "" {
				fmt.Fprintf(os.Stdout, "Public ID: %s\nURL: %s\n", publicID, entityURL)
			} else if identErr != nil {
				fmt.Fprintf(os.Stderr, "(created, but could not fetch public id/url: %v)\n", identErr)
			}
			return nil
		})
	},
}

func init() {
	createCmd.Flags().StringArrayVar(&createDocFields, "doc", nil, `set document field inline: --doc "Space/Description=# Heading\n\ncontent"`)
	createCmd.Flags().StringArrayVar(&createDocFiles, "doc-file", nil, `set document field from a file: --doc-file "Space/Description=path/to/body.md"`)
	createCmd.Flags().BoolVar(&createIDOnly, "id-only", false, "output only the fibery/id UUID (for scripting)")
	createCmd.Flags().BoolVar(&createPublicID, "public-id", false, "output only the public id (for scripting)")
	createCmd.Flags().BoolVar(&createSkipInvalid, "skip-invalid", false, "skip fields/values that don't resolve instead of failing the whole create")
	createCmd.Flags().BoolVar(&createMakeEnum, "create-missing-enum", false, "create absent enum values by name instead of failing")
	rootCmd.AddCommand(createCmd)
}
