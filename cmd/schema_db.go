package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

// schemaRef is one relation field: holder.field points at target.
type schemaRef struct {
	Holder string `json:"holder"`
	Field  string `json:"field"`
	Target string `json:"target"`
}

// dbComponents returns the helper types a database owns: its single-select
// enums ("Space/Priority_Space/DB") and its workflow ("workflow/state_Space/DB").
// Fibery names them "<prefix>_<db>", and they are not removed when the database
// is: schema.type/delete fails with schema-type-components-orphans unless they
// go in the same batch.
func dbComponents(schema map[string]any, db string) []string {
	var out []string
	for _, t := range mustSlice(schema["fibery/types"]) {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name := asStr(tm["fibery/name"])
		if name != db && strings.HasSuffix(name, "_"+db) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// dbRefs returns the relation fields leading out of db (to other databases and
// users) and into db (from other databases). Fields held by db's own component
// types are excluded from "in" — they are part of db, not references to it.
func dbRefs(schema map[string]any, db string) (out, in []schemaRef) {
	known := map[string]bool{"fibery/user": true}
	for _, d := range listDatabases(schema) {
		known[d] = true
	}
	comps := map[string]bool{}
	for _, c := range dbComponents(schema, db) {
		comps[c] = true
	}
	for _, t := range mustSlice(schema["fibery/types"]) {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		holder := asStr(tm["fibery/name"])
		if comps[holder] {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			ref := schemaRef{Holder: holder, Field: asStr(fm["fibery/name"]), Target: asStr(fm["fibery/type"])}
			switch {
			case holder == db && ref.Target != db && known[ref.Target]:
				out = append(out, ref)
			case holder != db && ref.Target == db:
				in = append(in, ref)
			}
		}
	}
	return out, in
}

// dbDeleteBatch builds the fibery.schema/batch sub-commands that delete db and
// its components in one atomic call. Component enums always drop their values
// (the option entities); the database's own entities only when deleteEntities.
func dbDeleteBatch(db string, components []string, deleteEntities bool) []any {
	cmds := []any{map[string]any{
		"command": "schema.type/delete",
		"args": map[string]any{
			"name":                   db,
			"delete-entities?":       deleteEntities,
			"delete-related-fields?": true,
		},
	}}
	for _, c := range components {
		cmds = append(cmds, map[string]any{
			"command": "schema.type/delete",
			"args": map[string]any{
				"name":                   c,
				"delete-entities?":       true,
				"delete-related-fields?": true,
			},
		})
	}
	return cmds
}

// requireDatabase errors unless db is a real database in the cached schema.
// Stricter than validateDB: a mutation must never pass an unknown name through.
func requireDatabase(schema map[string]any, db string) error {
	dbs := listDatabases(schema)
	if slices.Contains(dbs, db) {
		return nil
	}
	if s, ok := closestDatabase(db, dbs); ok {
		return fmt.Errorf("database %q not found — did you mean %q?", db, s)
	}
	return fmt.Errorf("database %q not found — run 'fibery schema sync' if it is new", db)
}

func printRefs(title string, refs []schemaRef, in bool) {
	fmt.Printf("\n### %s (%d)\n\n", title, len(refs))
	if len(refs) == 0 {
		fmt.Println("_none_")
		return
	}
	for _, r := range refs {
		if in {
			fmt.Printf("- %s · %s\n", r.Holder, r.Field)
		} else {
			fmt.Printf("- %s → %s\n", r.Field, r.Target)
		}
	}
}

var schemaRefsCmd = &cobra.Command{
	Use:   "refs <database>",
	Short: "Show a database's relations (out and in) and its helper types",
	Long: `List the relation fields leading out of a database, the fields in other
databases pointing at it, and the helper types it owns (single-select enums,
workflow). Read from the cached schema — run 'fibery schema sync' first if the
schema changed.

Example:
  fibery schema refs "Space/Database"
  fibery schema refs "Space/Database" --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		db := args[0]
		if err := requireDatabase(schema, db); err != nil {
			return err
		}
		out, in := dbRefs(schema, db)
		comps := dbComponents(schema, db)
		raw, _ := json.Marshal(map[string]any{"database": db, "out": out, "in": in, "components": comps})
		return outputJSON(raw, func() error {
			fmt.Printf("## %s\n", db)
			printRefs("Out", out, false)
			printRefs("In", in, true)
			fmt.Printf("\n### Helper types (%d)\n\n", len(comps))
			if len(comps) == 0 {
				fmt.Println("_none_")
			}
			for _, c := range comps {
				fmt.Printf("- %s\n", c)
			}
			return nil
		})
	},
}

var (
	dbDeleteYes            bool
	dbDeleteDryRun         bool
	dbDeleteDeleteEntities bool
)

var schemaDBCmd = &cobra.Command{
	Use:   "db",
	Short: "Create and delete databases",
}

var schemaDBDeleteCmd = &cobra.Command{
	Use:   "delete <database>",
	Short: "Delete a database together with its enums and workflow",
	Long: `Delete a database and the helper types it owns (single-select enums and
workflow states) in one atomic schema batch. Relation fields in other databases
that point at it are removed too.

Always prints the plan first: entity count, helper types with their value
counts, and every relation field that goes away. Nothing changes without --yes;
--dry-run prints the plan and stops even with --yes.

A database that still has entities is refused unless --delete-entities is given.
The schema cache is re-synced after a successful delete.

Examples:
  fibery schema db delete "Space/Database" --dry-run
  fibery schema db delete "Space/Database" --yes
  fibery schema db delete "Space/Database" --delete-entities --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		db := args[0]
		if err := requireDatabase(schema, db); err != nil {
			return err
		}
		n, err := countEntities(ctx, db, nil, nil)
		if err != nil {
			return fmt.Errorf("count %s: %w", db, err)
		}
		comps := dbComponents(schema, db)
		out, in := dbRefs(schema, db)

		fmt.Printf("## Delete %s\n\nEntities: %d\n", db, n)
		fmt.Printf("\n### Helper types (%d)\n\n", len(comps))
		if len(comps) == 0 {
			fmt.Println("_none_")
		}
		for _, c := range comps {
			cn, cErr := countEntities(ctx, c, nil, nil)
			if cErr != nil {
				fmt.Printf("- %s (values: ?)\n", c)
				continue
			}
			fmt.Printf("- %s (values: %d)\n", c, cn)
		}
		printRefs("Relations out (go with the database)", out, false)
		printRefs("Fields removed from other databases", in, true)
		fmt.Println()

		refused := n > 0 && !dbDeleteDeleteEntities
		if dbDeleteDryRun {
			if refused {
				fmt.Printf("Would be refused: %d entities — add --delete-entities to delete them too.\n", n)
			}
			fmt.Println("Dry run — nothing changed.")
			return nil
		}
		if refused {
			return fmt.Errorf("%s has %d entities — refusing; pass --delete-entities to delete them too", db, n)
		}
		if !dbDeleteYes {
			return fmt.Errorf("deleting a database is destructive — re-run with --yes to apply the plan above")
		}

		if _, err := cli.One(ctx, client.Command{
			Command: "fibery.schema/batch",
			Args:    map[string]any{"commands": dbDeleteBatch(db, comps, dbDeleteDeleteEntities)},
		}); err != nil {
			return fmt.Errorf("delete %s: %w", db, err)
		}
		fmt.Printf("Deleted %s (%d entities, %d helper types).\n", db, n, len(comps))
		if err := runSchemaSync(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: schema re-sync failed: %v — run 'fibery schema sync'\n", err)
		}
		return nil
	},
}

func init() {
	schemaDBDeleteCmd.Flags().BoolVar(&dbDeleteYes, "yes", false, "apply the plan (required — deleting a database is destructive)")
	schemaDBDeleteCmd.Flags().BoolVar(&dbDeleteDryRun, "dry-run", false, "print the plan and stop")
	schemaDBDeleteCmd.Flags().BoolVar(&dbDeleteDeleteEntities, "delete-entities", false, "also delete the database's entities (refused otherwise when it has any)")
	schemaDBCmd.AddCommand(schemaDBDeleteCmd)
	schemaCmd.AddCommand(schemaDBCmd)
	schemaCmd.AddCommand(schemaRefsCmd)
}
