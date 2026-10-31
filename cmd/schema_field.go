package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

// fieldSpec describes a field to create with `schema field add`.
type fieldSpec struct {
	DB       string
	Name     string // full "Space/Field"
	Kind     string // text|url|email|phone|int|decimal|bool|date|date-time|rich-text|single-select|relation|user
	Options  []string
	Default  string
	Required bool
	To       string // relation target database
	BackName string // relation: field created on the target ("Space/Field")
	Many     bool   // relation: this side holds many (many-to-many)
}

var primitiveKinds = map[string]string{
	"text":      "fibery/text",
	"url":       "fibery/text",
	"email":     "fibery/text",
	"phone":     "fibery/text",
	"int":       "fibery/int",
	"decimal":   "fibery/decimal",
	"bool":      "fibery/bool",
	"date":      "fibery/date",
	"date-time": "fibery/date-time",
}

// spaceOf returns the space part of "Space/Database".
func spaceOf(db string) string {
	return strings.SplitN(db, "/", 2)[0]
}

// qualifyField turns a bare field name into "Space/Name" using db's space.
func qualifyField(db, name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	return spaceOf(db) + "/" + name
}

// enumTypeName is the helper type Fibery uses for a single-select field:
// "${space}/${field}_${holder-type}".
func enumTypeName(db, field string) string {
	return field + "_" + db
}

// fieldAddCommands builds the top-level command for creating spec. It is
// always one fibery.command/batch (schema batch + option entities + default),
// so a single-select field lands with its options or not at all. newID makes
// UUIDs (injected so tests stay deterministic).
func fieldAddCommands(s fieldSpec, newID func() string) (client.Command, error) {
	if !strings.HasPrefix(s.Name, spaceOf(s.DB)+"/") {
		return client.Command{}, fmt.Errorf("field %q must be in the database's space %q", s.Name, spaceOf(s.DB))
	}
	var schemaCmds []any
	var after []any
	switch {
	case primitiveKinds[s.Kind] != "":
		meta := map[string]any{}
		if s.Kind == "url" || s.Kind == "email" || s.Kind == "phone" {
			meta["ui/type"] = s.Kind
		}
		schemaCmds = append(schemaCmds, fieldCreate(s.DB, s.Name, primitiveKinds[s.Kind], meta))
		if s.Kind == "bool" {
			// As the UI does: a checkbox is false, never empty, so filters on
			// "= false" match entities nobody ticked.
			after = append(after, schemaBatch([]any{
				setMeta(s.DB, s.Name, "fibery/default-value", false),
				setMeta(s.DB, s.Name, "fibery/required?", true),
			}))
		}
	case s.Kind == "rich-text":
		schemaCmds = append(schemaCmds, fieldCreate(s.DB, s.Name, "Collaboration~Documents/Document",
			map[string]any{"fibery/entity-component?": true}))
	case s.Kind == "single-select":
		if len(s.Options) == 0 {
			return client.Command{}, fmt.Errorf("single-select needs at least one --option")
		}
		enum := enumTypeName(s.DB, s.Name)
		schemaCmds = append(schemaCmds,
			map[string]any{"command": "schema.enum/create", "args": map[string]any{"fibery/name": enum}},
			fieldCreate(s.DB, s.Name, enum, nil),
		)
		defaultID := ""
		for i, o := range s.Options {
			id := newID()
			if strings.EqualFold(o, s.Default) {
				defaultID = id
			}
			after = append(after, map[string]any{
				"command": "fibery.entity/create",
				"args": map[string]any{"type": enum, "entity": map[string]any{
					"enum/name": o, "fibery/id": id, "fibery/rank": i * 1000000,
				}},
			})
		}
		if s.Default != "" && defaultID == "" {
			return client.Command{}, fmt.Errorf("--default %q is not one of the options", s.Default)
		}
		var metaCmds []any
		if defaultID != "" {
			metaCmds = append(metaCmds, setMeta(s.DB, s.Name, "fibery/default-value", map[string]any{"fibery/id": defaultID}))
		}
		if s.Required {
			if defaultID == "" {
				return client.Command{}, fmt.Errorf("--required single-select needs --default")
			}
			metaCmds = append(metaCmds, setMeta(s.DB, s.Name, "fibery/required?", true))
		}
		if len(metaCmds) > 0 {
			after = append(after, schemaBatch(metaCmds))
		}
	case s.Kind == "user":
		// A user field is a relation to fibery/user whose back field lives in the
		// "user" space, named the way the UI names it.
		rel := newID()
		here := map[string]any{"fibery/relation": rel}
		if s.Many {
			here["fibery/collection?"] = true
		}
		back := userBackField(s.DB, s.Name)
		schemaCmds = append(schemaCmds,
			fieldCreate(s.DB, s.Name, "fibery/user", here),
			fieldCreate("fibery/user", back, s.DB, map[string]any{"fibery/relation": rel, "fibery/collection?": true}),
		)
	case s.Kind == "relation":
		if s.To == "" {
			return client.Command{}, fmt.Errorf("relation needs --to <database>")
		}
		back := s.BackName
		if back == "" {
			back = spaceOf(s.To) + "/" + localName(s.DB)
		}
		if !strings.HasPrefix(back, spaceOf(s.To)+"/") {
			return client.Command{}, fmt.Errorf("--back-name %q must be in the target's space %q", back, spaceOf(s.To))
		}
		rel := newID()
		here := map[string]any{"fibery/relation": rel}
		if s.Many {
			here["fibery/collection?"] = true
		}
		schemaCmds = append(schemaCmds,
			fieldCreate(s.DB, s.Name, s.To, here),
			fieldCreate(s.To, back, s.DB, map[string]any{"fibery/relation": rel, "fibery/collection?": true}),
		)
	default:
		return client.Command{}, fmt.Errorf("unknown --type %q (text, url, email, phone, int, decimal, bool, date, date-time, rich-text, single-select, relation, user)", s.Kind)
	}
	if s.Required && s.Kind != "single-select" {
		return client.Command{}, fmt.Errorf("--required is only supported for single-select")
	}
	cmds := append([]any{schemaBatch(schemaCmds)}, after...)
	return client.Command{Command: "fibery.command/batch", Args: map[string]any{"commands": cmds}}, nil
}

// userBackField is the UI's name for the back side of a user field:
// "user/Tasks (Space - Assignee)".
func userBackField(db, field string) string {
	return fmt.Sprintf("user/%s (%s - %s)", localName(db), spaceOf(db), localName(field))
}

func fieldCreate(holder, name, typ string, meta map[string]any) map[string]any {
	args := map[string]any{"fibery/holder-type": holder, "fibery/name": name, "fibery/type": typ}
	if len(meta) > 0 {
		args["fibery/meta"] = meta
	}
	return map[string]any{"command": "schema.field/create", "args": args}
}

func setMeta(holder, name, key string, value any) map[string]any {
	return map[string]any{"command": "schema.field/set-meta", "args": map[string]any{
		"holder-type": holder, "name": name, "key": key, "value": value,
	}}
}

func schemaBatch(cmds []any) map[string]any {
	return map[string]any{"command": "fibery.schema/batch", "args": map[string]any{"commands": cmds}}
}

// schemaField returns the field map for db.field from the cached schema.
func findSchemaField(schema map[string]any, db, field string) map[string]any {
	for _, t := range mustSlice(schema["fibery/types"]) {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			if fm, ok := f.(map[string]any); ok && asStr(fm["fibery/name"]) == field {
				return fm
			}
		}
	}
	return nil
}

// relationPair finds the other side of a two-way relation field: the field in
// any type that shares fm's fibery/relation id.
func relationPair(schema map[string]any, holder string, fm map[string]any) (string, string) {
	meta, _ := fm["fibery/meta"].(map[string]any)
	rel := asStr(meta["fibery/relation"])
	if rel == "" {
		return "", ""
	}
	name := asStr(fm["fibery/name"])
	for _, t := range mustSlice(schema["fibery/types"]) {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		th := asStr(tm["fibery/name"])
		for _, f := range mustSlice(tm["fibery/fields"]) {
			om, ok := f.(map[string]any)
			if !ok {
				continue
			}
			ometa, _ := om["fibery/meta"].(map[string]any)
			if asStr(ometa["fibery/relation"]) == rel && (th != holder || asStr(om["fibery/name"]) != name) {
				return th, asStr(om["fibery/name"])
			}
		}
	}
	return "", ""
}

// fieldDeletePlan describes what `schema field delete` removes.
type fieldDeletePlan struct {
	Fields   []schemaRef // holder + field (Target = field type)
	EnumType string      // single-select helper type deleted with the field
	Batch    []any
}

// buildFieldDeletePlan resolves db.field against the schema: both sides of a
// relation go in one batch (Fibery requires it), and a single-select field takes
// its own enum type with it. Mixin and platform fields are refused.
func buildFieldDeletePlan(schema map[string]any, db, field string, deleteValues bool) (fieldDeletePlan, error) {
	var p fieldDeletePlan
	fm := findSchemaField(schema, db, field)
	if fm == nil {
		return p, fmt.Errorf("field %q not found in %s — run 'fibery schema sync' if it is new", field, db)
	}
	if !strings.HasPrefix(field, spaceOf(db)+"/") {
		return p, fmt.Errorf("%q is a built-in/mixin field — it cannot be deleted", field)
	}
	typ := asStr(fm["fibery/type"])
	p.Fields = append(p.Fields, schemaRef{Holder: db, Field: field, Target: typ})
	p.Batch = append(p.Batch, fieldDelete(db, field, deleteValues))
	if h, f := relationPair(schema, db, fm); h != "" && !isEnumLikeType(typ) {
		p.Fields = append(p.Fields, schemaRef{Holder: h, Field: f, Target: db})
		p.Batch = append(p.Batch, fieldDelete(h, f, deleteValues))
	}
	if typ == enumTypeName(db, field) {
		p.EnumType = typ
		p.Batch = append(p.Batch, map[string]any{"command": "schema.type/delete", "args": map[string]any{
			"name": typ, "delete-entities?": true, "delete-related-fields?": true,
		}})
	}
	return p, nil
}

func fieldDelete(holder, name string, deleteValues bool) map[string]any {
	return map[string]any{"command": "schema.field/delete", "args": map[string]any{
		"holder-type": holder, "name": name, "delete-values?": deleteValues,
	}}
}

var (
	fieldAddSpec                fieldSpec
	fieldAddDryRun, fieldAddYes bool

	fieldDelYes, fieldDelDryRun, fieldDelValues bool
	renameYes, renameDryRun                     bool
)

var schemaFieldCmd = &cobra.Command{
	Use:   "field",
	Short: "Add, rename and delete fields",
}

var schemaFieldAddCmd = &cobra.Command{
	Use:   "add <database> <field>",
	Short: "Add a field (primitive, rich text, single-select or relation)",
	Long: `Add a field to a database. A bare field name gets the database's space
("Priority" → "Space/Priority"). Single-select options are created in the same
batch; a relation creates both sides (the back field on --to is a collection).
Prints the plan; nothing changes without --yes.

Examples:
  fibery schema field add "Space/Database" Estimate --type int --yes
  fibery schema field add "Space/Database" Link --type url
  fibery schema field add "Space/Database" Notes --type rich-text
  fibery schema field add "Space/Database" Project --type single-select \
    --option Alpha --option Beta --default Alpha
  fibery schema field add "Space/Database" Assignee --type user --many
  fibery schema field add "Space/Database" Epic --type relation --to "Space/Epic"
  fibery schema field add "Space/Database" Tags --type relation --to "Space/Tag" --many --dry-run`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		s := fieldAddSpec
		s.DB = args[0]
		s.Name = qualifyField(s.DB, args[1])
		if err := requireDatabase(schema, s.DB); err != nil {
			return err
		}
		if findSchemaField(schema, s.DB, s.Name) != nil {
			return fmt.Errorf("%s already has field %q", s.DB, s.Name)
		}
		if s.Kind == "relation" && s.To != "" {
			if err := requireDatabase(schema, s.To); err != nil {
				return err
			}
		}
		c, err := fieldAddCommands(s, client.NewUUID)
		if err != nil {
			return err
		}
		fmt.Printf("## Add %s · %s (%s)\n", s.DB, s.Name, s.Kind)
		switch s.Kind {
		case "single-select":
			fmt.Printf("Options: %s", strings.Join(s.Options, ", "))
			if s.Default != "" {
				fmt.Printf(" · default %s", s.Default)
			}
			if s.Required {
				fmt.Print(" · required")
			}
			fmt.Println()
		case "relation":
			back := s.BackName
			if back == "" {
				back = spaceOf(s.To) + "/" + localName(s.DB)
			}
			many := "one"
			if s.Many {
				many = "many"
			}
			fmt.Printf("→ %s (%s per entity) · back field %s · %s (many)\n", s.To, many, s.To, back)
		}
		if fieldAddDryRun {
			fmt.Println("Dry run — nothing changed.")
			return nil
		}
		if !fieldAddYes {
			return fmt.Errorf("re-run with --yes to add the field above")
		}
		if _, err := cli.One(ctx, c); err != nil {
			return fmt.Errorf("add field: %w", err)
		}
		fmt.Println("Added.")
		resync(cmd)
		return nil
	},
}

var schemaFieldDeleteCmd = &cobra.Command{
	Use:   "delete <database> <field>",
	Short: "Delete a field (both sides of a relation, a single-select's options)",
	Long: `Delete a field. A relation loses both sides in one batch; a single-select
field takes its option type with it. Built-in/mixin fields (workflow/state,
comments, files, assignees, fibery/*) are refused.

A field that still holds values is refused by Fibery unless --delete-values is
given. Prints the plan first; nothing changes without --yes.

Examples:
  fibery schema field delete "Space/Database" Estimate --dry-run
  fibery schema field delete "Space/Database" Estimate --delete-values --yes`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		db := args[0]
		if err := requireDatabase(schema, db); err != nil {
			return err
		}
		p, err := buildFieldDeletePlan(schema, db, qualifyField(db, args[1]), fieldDelValues)
		if err != nil {
			return err
		}
		fmt.Printf("## Delete field\n\n")
		for _, f := range p.Fields {
			fmt.Printf("- %s · %s (%s)\n", f.Holder, f.Field, f.Target)
		}
		if p.EnumType != "" {
			fmt.Printf("- option type %s\n", p.EnumType)
		}
		fmt.Printf("Values: %s\n\n", map[bool]string{true: "deleted", false: "must be empty (else Fibery refuses)"}[fieldDelValues])
		if fieldDelDryRun {
			fmt.Println("Dry run — nothing changed.")
			return nil
		}
		if !fieldDelYes {
			return fmt.Errorf("deleting a field is destructive — re-run with --yes to apply the plan above")
		}
		if _, err := cli.One(cmd.Context(), client.Command{Command: "fibery.schema/batch", Args: map[string]any{"commands": p.Batch}}); err != nil {
			return fmt.Errorf("delete field: %w", err)
		}
		fmt.Println("Deleted.")
		resync(cmd)
		return nil
	},
}

var schemaFieldRenameCmd = &cobra.Command{
	Use:   "rename <database> <field> <new-name>",
	Short: "Rename a field",
	Long: `Rename a field (bare names get the database's space). Values are kept.

Example:
  fibery schema field rename "Space/Database" Estimate Points --yes`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		db := args[0]
		if err := requireDatabase(schema, db); err != nil {
			return err
		}
		from, to := qualifyField(db, args[1]), qualifyField(db, args[2])
		if findSchemaField(schema, db, from) == nil {
			return fmt.Errorf("field %q not found in %s", from, db)
		}
		if findSchemaField(schema, db, to) != nil {
			return fmt.Errorf("%s already has field %q", db, to)
		}
		return applyRename(cmd, fmt.Sprintf("Rename field %s · %s → %s", db, from, to),
			map[string]any{"command": "schema.field/rename", "args": map[string]any{"holder-type": db, "from-name": from, "to-name": to}})
	},
}

var schemaDBRenameCmd = &cobra.Command{
	Use:   "rename <database> <new-name>",
	Short: "Rename a database (entities, ids and fields are kept)",
	Long: `Rename a database within its space. A bare new name keeps the space
("Tasks" → "Space/Tasks").

Example:
  fibery schema db rename "Space/Task" Tasks --yes`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		from := args[0]
		if err := requireDatabase(schema, from); err != nil {
			return err
		}
		to := qualifyField(from, args[1])
		if spaceOf(to) != spaceOf(from) {
			return fmt.Errorf("a database cannot change space (%q → %q)", spaceOf(from), spaceOf(to))
		}
		if requireDatabase(schema, to) == nil {
			return fmt.Errorf("database %q already exists", to)
		}
		return applyRename(cmd, fmt.Sprintf("Rename database %s → %s", from, to),
			map[string]any{"command": "schema.type/rename", "args": map[string]any{"from-name": from, "to-name": to}})
	},
}

func applyRename(cmd *cobra.Command, plan string, c map[string]any) error {
	fmt.Println("## " + plan)
	if renameDryRun {
		fmt.Println("Dry run — nothing changed.")
		return nil
	}
	if !renameYes {
		return fmt.Errorf("re-run with --yes to apply the rename above")
	}
	if _, err := cli.One(cmd.Context(), client.Command{Command: "fibery.schema/batch", Args: map[string]any{"commands": []any{c}}}); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Println("Renamed.")
	resync(cmd)
	return nil
}

func resync(cmd *cobra.Command) {
	if err := runSchemaSync(cmd.Context()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: schema re-sync failed: %v — run 'fibery schema sync'\n", err)
	}
}

func init() {
	f := schemaFieldAddCmd.Flags()
	f.StringVar(&fieldAddSpec.Kind, "type", "", "text|url|email|phone|int|decimal|bool|date|date-time|rich-text|single-select|relation|user (required)")
	f.StringArrayVar(&fieldAddSpec.Options, "option", nil, "single-select option (repeatable, in order)")
	f.StringVar(&fieldAddSpec.Default, "default", "", "single-select default option")
	f.BoolVar(&fieldAddSpec.Required, "required", false, "single-select: value required (needs --default)")
	f.StringVar(&fieldAddSpec.To, "to", "", "relation: target database")
	f.StringVar(&fieldAddSpec.BackName, "back-name", "", "relation: field name on the target (default: this database's name)")
	f.BoolVar(&fieldAddSpec.Many, "many", false, "relation: many-to-many (default: each entity links one target)")
	f.BoolVar(&fieldAddDryRun, "dry-run", false, "print the plan and stop")
	f.BoolVar(&fieldAddYes, "yes", false, "apply (required)")
	_ = schemaFieldAddCmd.MarkFlagRequired("type")

	d := schemaFieldDeleteCmd.Flags()
	d.BoolVar(&fieldDelYes, "yes", false, "apply the plan (required)")
	d.BoolVar(&fieldDelDryRun, "dry-run", false, "print the plan and stop")
	d.BoolVar(&fieldDelValues, "delete-values", false, "also delete the values the field holds")

	for _, c := range []*cobra.Command{schemaFieldRenameCmd, schemaDBRenameCmd} {
		c.Flags().BoolVar(&renameYes, "yes", false, "apply the rename (required)")
		c.Flags().BoolVar(&renameDryRun, "dry-run", false, "print the plan and stop")
	}

	schemaFieldCmd.AddCommand(schemaFieldAddCmd, schemaFieldDeleteCmd, schemaFieldRenameCmd)
	schemaDBCmd.AddCommand(schemaDBRenameCmd)
	schemaCmd.AddCommand(schemaFieldCmd)
}
