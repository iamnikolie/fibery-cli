package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var viewCmd = &cobra.Command{
	Use:   "view",
	Short: "Create, list and delete views (lists, tables), incl. views embedded in rich text",
	Long: `Work with Fibery views through the undocumented views JSON-RPC endpoint
(/api/views/json-rpc), which may change without notice.`,
}

var (
	viewCreateDB       string
	viewCreateName     string
	viewCreateType     string
	viewCreateFilters  []string
	viewCreateAny      bool
	viewCreateWhere    string
	viewCreateParams   string
	viewCreateEmbed    string
	viewCreateEmbedDB  string
	viewCreateEmbedFld string
	viewCreateAfter    string
)

var viewCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a list/table view over a database, optionally embedded in an entity's rich text",
	Long: `Create a list or table view over --db. Filters use the same "field<op>value"
syntax as 'fibery query --filter' (ops: = != ~) and are stored the way the Fibery
filter editor stores them, so they stay editable in the UI:
  - text/number/bool/date fields: = and != compare the value, ~ is "contains"
    (case-sensitive, like the UI);
  - workflow state / enum fields match by value name, user fields by email,
    relation fields by the related entity's public id (= and != only).
Several --filter flags are ANDed; --any ORs them. --where takes a raw FQL
where-expression (field names are translated to ids; sub-queries must use ids)
with --params.

Without --embed the view lands in the left nav of the database's space. With
--embed it is owned by that entity and inserted into its rich-text field
(--embed-field, default: the entity's first rich-text field) as an inline view,
like "/view" in the editor: at the end of the section under --after-heading, or
at the end of the document.

Examples:
  fibery view create --db "Space/Database" --name "Open UI bugs" --filter "Space/Name~[UI]"
  fibery view create --db "Space/Database" --name "Mine" --type table \
    --filter "workflow/state=Open" --filter "Space/Owner=dev@acme.com"
  fibery view create --db "Space/Database" --name "[ii] bugs" --filter "Space/Name~[ii]" \
    --embed 3947 --embed-db "Space/Database" --embed-field "Space/Description" --after-heading Scope`,
	Args: cobra.NoArgs,
	RunE: runViewCreate,
}

func runViewCreate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if viewCreateDB == "" || viewCreateName == "" {
		return fmt.Errorf("--db and --name are required")
	}
	if !viewTypes[viewCreateType] {
		return fmt.Errorf("--type must be list or table, got %q", viewCreateType)
	}
	if viewCreateWhere != "" && len(viewCreateFilters) > 0 {
		return fmt.Errorf("--where and --filter are mutually exclusive")
	}
	if err := validateDB(viewCreateDB); err != nil {
		return err
	}
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return err
	}
	dbTypeID := findTypeID(schema, viewCreateDB)
	if dbTypeID == "" {
		return fmt.Errorf("database %q not in schema cache (run 'fibery schema sync')", viewCreateDB)
	}

	filters, err := viewCreateFilterLeaves(ctx, schema)
	if err != nil {
		return err
	}

	var target *embedTarget
	var owner *viewOwner
	if viewCreateEmbed != "" {
		target, err = resolveEmbedTarget(cmd, schema, viewCreateEmbed, viewCreateEmbedDB, viewCreateEmbedFld)
		if err != nil {
			return err
		}
		owner = &target.Owner
		doc, err := cli.GetDocumentJSON(ctx, target.Secret)
		if err != nil {
			return err
		}
		if err := checkInsertPoint(doc.Content.Doc, viewCreateAfter); err != nil {
			return err
		}
	}

	appDB := viewCreateDB
	if target != nil {
		appDB = target.DB
	}
	appID, err := appIDForDB(ctx, appDB)
	if err != nil {
		return err
	}

	spec := viewSpec{
		ID:      client.NewUUID(),
		Name:    viewCreateName,
		Type:    viewCreateType,
		DBType:  dbTypeID,
		AppID:   appID,
		Filters: filters,
		AnyOf:   viewCreateAny,
		Units:   defaultViewUnits(schema, viewCreateDB),
		OrderBy: defaultViewOrder(schema, viewCreateDB),
		Owner:   owner,
	}
	if err := cli.CreateViews(ctx, []map[string]any{buildViewRecord(spec)}); err != nil {
		if owner == nil && strings.Contains(err.Error(), "create view permission") {
			return fmt.Errorf("%w — a left-nav view needs view-creation rights on the space; --embed creates one owned by an entity instead", err)
		}
		return err
	}
	created, err := cli.QueryViews(ctx, map[string]any{"ids": []string{spec.ID}})
	if err != nil || len(created) == 0 {
		return fmt.Errorf("view %s was sent to create-views but cannot be read back: %v", spec.ID, err)
	}
	v := created[0]

	if target != nil {
		if err := embedViewInDoc(ctx, target.Secret, v.ID, viewCreateAfter); err != nil {
			return fmt.Errorf("view %q (#%s, id %s) was created but not embedded: %w — delete it with 'fibery view delete %s --yes' or embed it by hand",
				v.Name, v.PublicID, v.ID, err, v.ID)
		}
	}

	if outputFormat == "json" || jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]string{
			"id": v.ID, "publicId": v.PublicID, "name": v.Name, "type": v.Type,
		})
	}
	fmt.Printf("Created %s view %q (#%s, id %s)", v.Type, v.Name, v.PublicID, v.ID)
	if target != nil {
		fmt.Printf(", embedded in %s #%s → %s", target.DB, target.Owner.PublicID, target.Field)
	}
	fmt.Println(".")
	return nil
}

// viewCreateFilterLeaves builds the filter leaves from --filter or --where.
func viewCreateFilterLeaves(ctx context.Context, schema map[string]any) ([]viewFilter, error) {
	if viewCreateWhere != "" {
		var where any
		if err := json.Unmarshal([]byte(viewCreateWhere), &where); err != nil {
			return nil, fmt.Errorf("--where is not valid JSON: %w", err)
		}
		expr, err := translateWhereNames(schema, viewCreateDB, where)
		if err != nil {
			return nil, err
		}
		params := map[string]any{}
		if viewCreateParams != "" {
			if err := json.Unmarshal([]byte(viewCreateParams), &params); err != nil {
				return nil, fmt.Errorf("--params is not a JSON object: %w", err)
			}
		}
		return []viewFilter{{Expression: expr, Params: params}}, nil
	}
	return buildViewFilters(schema, viewCreateDB, viewCreateFilters, func(f schemaField, value string) (string, error) {
		return resolveRefValueID(ctx, f, value)
	})
}

// resolveRefValueID looks up the fibery/id behind a reference filter value.
func resolveRefValueID(ctx context.Context, f schemaField, value string) (string, error) {
	if isUUID(value) {
		return value, nil
	}
	switch {
	case f.Type == "fibery/user":
		ref, err := resolveUserByEmail(ctx, value)
		if err != nil {
			return "", err
		}
		return asStr(ref["fibery/id"]), nil
	case isEnumLikeType(f.Type):
		ref, err := resolveEnumByName(ctx, f.Type, value)
		if err != nil {
			return "", err
		}
		return asStr(ref["fibery/id"]), nil
	default:
		return resolveEntityRef(ctx, f.Type, value)
	}
}

// embedTarget is a rich-text field of an entity that a view is embedded into.
type embedTarget struct {
	DB     string
	Field  string
	Secret string
	Owner  viewOwner
}

// resolveEmbedTarget resolves an entity reference (URL, UUID, "42", "DT-42") and
// one of its rich-text fields (default: the first) to the document to embed into.
// It runs before any view is created, so a bad target leaves nothing behind.
func resolveEmbedTarget(cmd *cobra.Command, schema map[string]any, ref, db, field string) (*embedTarget, error) {
	ctx := cmd.Context()
	var uuid string
	var err error
	if looksLikeURL(ref) {
		db, uuid, err = resolveURLToEntityID(cmd, ref)
	} else {
		if db == "" {
			return nil, fmt.Errorf("--embed-db is required unless --embed is a Fibery URL")
		}
		uuid, err = resolveEntityRef(ctx, db, ref)
	}
	if err != nil {
		return nil, err
	}
	typeID := findTypeID(schema, db)
	if typeID == "" {
		return nil, fmt.Errorf("database %q not in schema cache (run 'fibery schema sync')", db)
	}
	if field == "" {
		f, ok := findDescriptionField(schema, db)
		if !ok {
			return nil, fmt.Errorf("%s has no rich-text field", db)
		}
		field = f
	}
	field = resolveFieldName(schema, db, field)

	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": map[string]any{"pid": "fibery/public-id"},
				"q/where":  []any{"=", []any{"fibery/id"}, "$id"},
				"q/limit":  1,
			},
			"params": map[string]any{"$id": uuid},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", ref, err)
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("resolve %q: parse: %w", ref, err)
	}
	if len(items) == 0 {
		return nil, notFoundRefErr(ref, db)
	}
	secret, err := resolveDocSecretByID(ctx, db, uuid, field)
	if err != nil {
		return nil, err
	}
	return &embedTarget{
		DB:     db,
		Field:  field,
		Secret: secret,
		Owner:  viewOwner{TypeID: typeID, UUID: uuid, PublicID: asStr(items[0]["pid"])},
	}, nil
}

// appIDForDB returns the fibery/id of the space (app) a database belongs to.
func appIDForDB(ctx context.Context, db string) (string, error) {
	space, _, ok := strings.Cut(db, "/")
	if !ok {
		return "", fmt.Errorf("database %q has no space prefix", db)
	}
	apps, err := loadAppNames(ctx)
	if err != nil {
		return "", err
	}
	for id, name := range apps {
		if name == space {
			return id, nil
		}
	}
	return "", fmt.Errorf("space %q not found", space)
}

// embedViewInDoc inserts an inline-view node into the document, through the JSON
// endpoint so the rest of the body and its inline comments are left untouched.
func embedViewInDoc(ctx context.Context, secret, viewID, afterHeading string) error {
	doc, err := cli.GetDocumentJSON(ctx, secret)
	if err != nil {
		return err
	}
	if err := insertDocBlock(&doc.Content, viewEmbedNode(viewID), afterHeading); err != nil {
		return err
	}
	return cli.SetDocumentJSON(ctx, secret, doc.Content)
}

var (
	viewEmbedRef   string
	viewEmbedDB    string
	viewEmbedField string
	viewEmbedAfter string
)

var viewEmbedCmd = &cobra.Command{
	Use:   "embed <view uuid|public-id>",
	Short: "Embed an existing view into an entity's rich-text field",
	Long: `Insert an existing view into a rich-text field as an inline view, at the end of
the section under --after-heading or at the end of the document. Refuses a view
that is already embedded in that document.

Examples:
  fibery view embed 2930 --embed 3947 --embed-db "Space/Database" --embed-field "Space/Description"
  fibery view embed 2930 --embed https://acme.fibery.io/Space/Database/Title-3947 --after-heading Scope`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if viewEmbedRef == "" {
			return fmt.Errorf("--embed is required")
		}
		v, err := findView(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return err
		}
		target, err := resolveEmbedTarget(cmd, schema, viewEmbedRef, viewEmbedDB, viewEmbedField)
		if err != nil {
			return err
		}
		if err := embedViewInDoc(cmd.Context(), target.Secret, v.ID, viewEmbedAfter); err != nil {
			return err
		}
		fmt.Printf("Embedded %s view %q (#%s) in %s #%s → %s.\n", v.Type, v.Name, v.PublicID, target.DB, target.Owner.PublicID, target.Field)
		return nil
	},
}

// findView resolves a view by UUID or public id.
func findView(ctx context.Context, ref string) (*client.ViewRecord, error) {
	filter := map[string]any{"publicIds": []string{ref}}
	if isUUID(ref) {
		filter = map[string]any{"ids": []string{ref}}
	}
	views, err := cli.QueryViews(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, fmt.Errorf("view %q not found", ref)
	}
	return &views[0], nil
}

var (
	viewListSpace string
	viewListType  string
	viewListOwner string
)

var viewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List views (non-document), optionally by space, type or owning entity",
	Long: `List views across the workspace. Space/wiki documents are listed by 'fibery wiki list'.

Examples:
  fibery view list --space Development
  fibery view list --type list
  fibery view list --owner 3947        # views owned by (embedded in) entity #3947`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		appNames, err := loadAppNames(cmd.Context())
		if err != nil {
			return err
		}
		views, err := cli.QueryViews(cmd.Context(), nil)
		if err != nil {
			return err
		}
		rows := filterViews(views, appNames, viewListSpace, viewListType, viewListOwner)
		if outputFormat == "json" || jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		fmt.Printf("| View | ID | Type | Space | Owner | UUID |\n|------|----|------|-------|-------|------|\n")
		for _, r := range rows {
			fmt.Printf("| %s | %s | %s | %s | %s | %s |\n", r["name"], r["publicId"], r["type"], r["space"], r["owner"], r["id"])
		}
		return nil
	},
}

// filterViews keeps non-document views matching the space / type / owner filters.
func filterViews(views []client.ViewRecord, appNames map[string]string, space, typ, owner string) []map[string]string {
	rows := []map[string]string{}
	for _, v := range views {
		if v.Type == "document" {
			continue
		}
		sp := appNames[v.ContainerAppID]
		if space != "" && !strings.EqualFold(sp, space) {
			continue
		}
		if typ != "" && v.Type != typ {
			continue
		}
		if owner != "" && v.ContainerEntityID != owner {
			continue
		}
		rows = append(rows, map[string]string{
			"name": v.Name, "publicId": v.PublicID, "type": v.Type, "space": sp,
			"owner": v.ContainerEntityID, "id": v.ID,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i]["space"] < rows[j]["space"] })
	return rows
}

var viewDeleteYes bool

var viewDeleteCmd = &cobra.Command{
	Use:   "delete <uuid|public-id>",
	Short: "Delete a view",
	Long: `Delete a view by UUID or public id. Requires --yes. For a view owned by an entity
(one embedded with --embed), its inline-view blocks are also removed from that
entity's rich-text fields.

Example:
  fibery view delete 2901 --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := findView(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if !viewDeleteYes {
			return fmt.Errorf("delete is destructive — add --yes to confirm (%s view %q, #%s)", v.Type, v.Name, v.PublicID)
		}
		if err := cli.DeleteViews(cmd.Context(), []string{v.ID}); err != nil {
			return err
		}
		fmt.Printf("Deleted %s view %q (#%s).\n", v.Type, v.Name, v.PublicID)
		if v.ContainerType != "object" || v.ContainerEntityID == "" {
			return nil
		}
		n, err := removeOwnerEmbeds(cmd.Context(), v)
		if err != nil {
			return fmt.Errorf("view deleted, but its block in entity #%s was not removed: %w", v.ContainerEntityID, err)
		}
		if n > 0 {
			fmt.Printf("Removed %d inline-view block(s) from entity #%s.\n", n, v.ContainerEntityID)
		}
		return nil
	},
}

// removeOwnerEmbeds removes a view's inline blocks from every rich-text field of
// the entity that owns it.
func removeOwnerEmbeds(ctx context.Context, v *client.ViewRecord) (int, error) {
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return 0, err
	}
	db := typeNameByID(schema, v.ContainerEntityTypeID)
	if db == "" {
		return 0, fmt.Errorf("owner type %s not in schema cache", v.ContainerEntityTypeID)
	}
	uuid, err := resolveEntityRef(ctx, db, v.ContainerEntityID)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, field := range richTextFields(schema, db) {
		secret, err := resolveDocSecretByID(ctx, db, uuid, field)
		if err != nil {
			continue // field has no document yet
		}
		doc, err := cli.GetDocumentJSON(ctx, secret)
		if err != nil {
			return total, err
		}
		n, err := removeViewEmbeds(&doc.Content, v.ID)
		if err != nil {
			return total, err
		}
		if n == 0 {
			continue
		}
		if err := cli.SetDocumentJSON(ctx, secret, doc.Content); err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func init() {
	f := viewCreateCmd.Flags()
	f.StringVar(&viewCreateDB, "db", "", "database the view shows (required)")
	f.StringVar(&viewCreateName, "name", "", "view name (required)")
	f.StringVar(&viewCreateType, "type", "list", "view type: list | table")
	f.StringArrayVar(&viewCreateFilters, "filter", nil, `filter "field=value" (repeatable; ops: = != ~)`)
	f.BoolVar(&viewCreateAny, "any", false, "OR the --filter clauses instead of AND")
	f.StringVar(&viewCreateWhere, "where", "", "raw FQL where-expression as JSON (instead of --filter)")
	f.StringVar(&viewCreateParams, "params", "", "JSON object of params for --where")
	f.StringVar(&viewCreateEmbed, "embed", "", "entity to embed the view in: URL, UUID, 42 or DT-42")
	f.StringVar(&viewCreateEmbedDB, "embed-db", "", "database of --embed (unless --embed is a URL)")
	f.StringVar(&viewCreateEmbedFld, "embed-field", "", "rich-text field to embed into (default: the entity's first rich-text field)")
	f.StringVar(&viewCreateAfter, "after-heading", "", "insert at the end of the section under this heading (default: end of document)")

	e := viewEmbedCmd.Flags()
	e.StringVar(&viewEmbedRef, "embed", "", "entity to embed into: URL, UUID, 42 or DT-42 (required)")
	e.StringVar(&viewEmbedDB, "embed-db", "", "database of --embed (unless --embed is a URL)")
	e.StringVar(&viewEmbedField, "embed-field", "", "rich-text field to embed into (default: the entity's first rich-text field)")
	e.StringVar(&viewEmbedAfter, "after-heading", "", "insert at the end of the section under this heading (default: end of document)")

	viewListCmd.Flags().StringVar(&viewListSpace, "space", "", "only views in this space")
	viewListCmd.Flags().StringVar(&viewListType, "type", "", "only views of this type (list, table, board, …)")
	viewListCmd.Flags().StringVar(&viewListOwner, "owner", "", "only views owned by the entity with this public id")

	viewDeleteCmd.Flags().BoolVar(&viewDeleteYes, "yes", false, "confirm deletion (required)")

	viewCmd.AddCommand(viewCreateCmd, viewEmbedCmd, viewListCmd, viewDeleteCmd)
	rootCmd.AddCommand(viewCmd)
}
