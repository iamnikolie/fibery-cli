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

// dbCreateSpec describes a database to create with `schema db create`.
type dbCreateSpec struct {
	DB          string // "Space/Name"
	Color       string
	Workflow    bool
	Comments    bool
	Files       bool
	Description bool
	States      []stateSpec
}

// stateSpec is one workflow state. Type is the Fibery workflow type
// ("Not started", "Started", "Finished"); it can only be set when the state is
// created.
type stateSpec struct {
	Name string
	Type string
}

var workflowTypes = map[string]string{
	"not-started": "Not started",
	"started":     "Started",
	"finished":    "Finished",
}

// stateTypeByName guesses a workflow type from common state names.
var stateTypeByName = map[string]string{
	"backlog": "Not started", "to do": "Not started", "todo": "Not started", "open": "Not started", "new": "Not started", "ready": "Not started",
	"done": "Finished", "closed": "Finished", "released": "Finished", "rejected": "Finished", "cancelled": "Finished", "canceled": "Finished", "won't do": "Finished", "wontfix": "Finished",
}

// parseStates turns "Name[=type]" flags into states. Without a type a common
// name decides (Backlog, To Do → not-started; Done, Rejected → finished);
// otherwise the first state is not-started, the last finished, the rest started.
func parseStates(raw []string) ([]stateSpec, error) {
	out := make([]stateSpec, 0, len(raw))
	seen := map[string]bool{}
	for i, r := range raw {
		name, typ, hasType := strings.Cut(r, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("--state %q has no name", r)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("--state %q given twice", name)
		}
		seen[strings.ToLower(name)] = true
		var t string
		switch {
		case hasType:
			t = workflowTypes[strings.ToLower(strings.TrimSpace(typ))]
			if t == "" {
				return nil, fmt.Errorf("--state %q: type must be not-started, started or finished", r)
			}
		case stateTypeByName[strings.ToLower(name)] != "":
			t = stateTypeByName[strings.ToLower(name)]
		case i == 0:
			t = "Not started"
		case i == len(raw)-1:
			t = "Finished"
		default:
			t = "Started"
		}
		out = append(out, stateSpec{Name: name, Type: t})
	}
	if len(out) > 0 && out[0].Type != "Not started" {
		return nil, fmt.Errorf("the first --state is the default for new entities and must be not-started")
	}
	return out, nil
}

// dbCreateBatch builds the schema batch that creates the database with its
// mandatory fields and mixins. Two traps found against the live API: the title
// field must not carry fibery/secured? false (the documented example does, and
// is refused with schema-type-fields-not-secured), and attachments are a
// fibery/file collection field, not a mixin.
func dbCreateBatch(s dbCreateSpec) ([]any, error) {
	space, name, ok := strings.Cut(s.DB, "/")
	if !ok || space == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("database must be \"Space/Name\", got %q", s.DB)
	}
	if strings.Contains(name, "_") {
		return nil, fmt.Errorf("database name %q must not contain \"_\" (Fibery reserves it for helper types)", name)
	}
	color := s.Color
	if color == "" {
		color = "#4FAF54"
	}
	field := func(n, typ string, meta map[string]any) map[string]any {
		return map[string]any{"fibery/name": n, "fibery/type": typ, "fibery/meta": meta}
	}
	fields := []any{
		field(space+"/Name", "fibery/text", map[string]any{"ui/title?": true}),
		field("fibery/id", "fibery/uuid", map[string]any{"fibery/secured?": false, "fibery/id?": true, "fibery/readonly?": true}),
		field("fibery/public-id", "fibery/text", map[string]any{"fibery/secured?": false, "fibery/public-id?": true, "fibery/readonly?": true}),
		field("fibery/creation-date", "fibery/date-time", map[string]any{"fibery/secured?": false, "fibery/creation-date?": true, "fibery/readonly?": true, "fibery/default-value": "$now"}),
		field("fibery/modification-date", "fibery/date-time", map[string]any{"fibery/secured?": false, "fibery/modification-date?": true, "fibery/required?": true, "fibery/readonly?": true, "fibery/default-value": "$now"}),
	}
	if s.Description {
		fields = append(fields, field(space+"/Description", "Collaboration~Documents/Document", map[string]any{"fibery/entity-component?": true}))
	}
	if s.Files {
		fields = append(fields, field(space+"/Files", "fibery/file", map[string]any{"fibery/collection?": true, "fibery/entity-component?": true}))
	}
	mixins := []any{"fibery/rank-mixin", "Collaboration~Documents/ReferencesMixin"}
	if s.Workflow {
		mixins = append(mixins, "workflow/workflow")
	}
	if s.Comments {
		mixins = append(mixins, "comments/comments-mixin")
	}
	return []any{
		map[string]any{"command": "schema.type/create", "args": map[string]any{
			"fibery/name":   s.DB,
			"fibery/meta":   map[string]any{"fibery/domain?": true, "fibery/secured?": true, "ui/color": color},
			"fibery/fields": fields,
		}},
		map[string]any{"command": "fibery.app/install-mixins", "args": map[string]any{
			"types": map[string]any{s.DB: mixins},
		}},
	}, nil
}

// stateReplaceCommands swaps the workflow's built-in states (To Do, In
// Progress, Done) for states: create the new ones with their type (settable
// only at create), point the field default at the first, delete the old ones
// (the default one cannot be deleted while it is the default).
func stateReplaceCommands(db string, states []stateSpec, oldIDs []string, newID func() string) client.Command {
	stateType := "workflow/state_" + db
	var cmds []any
	first := ""
	for i, st := range states {
		id := newID()
		if i == 0 {
			first = id
		}
		cmds = append(cmds, map[string]any{"command": "fibery.entity/create", "args": map[string]any{
			"type": stateType,
			"entity": map[string]any{
				"fibery/id": id, "enum/name": st.Name, "workflow/Type": st.Type,
				"workflow/Final": st.Type == "Finished", "fibery/rank": (i + 1) * 100000,
			},
		}})
	}
	cmds = append(cmds, schemaBatch([]any{setMeta(db, "workflow/state", "fibery/default-value", map[string]any{"fibery/id": first})}))
	for _, id := range oldIDs {
		cmds = append(cmds, map[string]any{"command": "fibery.entity/delete", "args": map[string]any{
			"type": stateType, "entity": map[string]any{"fibery/id": id},
		}})
	}
	return client.Command{Command: "fibery.command/batch", Args: map[string]any{"commands": cmds}}
}

var (
	dbCreateSpecFlags                 dbCreateSpec
	dbCreateStates                    []string
	dbCreateNoWorkflow, dbCreateNoCom bool
	dbCreateNoFiles, dbCreateNoDesc   bool
	dbCreateYes, dbCreateDryRun       bool
)

var schemaDBCreateCmd = &cobra.Command{
	Use:   "create <database>",
	Short: "Create a database with workflow, comments, files and a description",
	Long: `Create "Space/Name" in an existing space: the mandatory fields, Name as the
title, a Description rich-text field, Files, and the rank, references, workflow
and comments extensions — what the UI gives a new database. --no-* drops a part.

--state replaces the built-in workflow (To Do, In Progress, Done). Each is
"Name[=not-started|started|finished]"; the type is fixed at creation. Without a
type a common name decides (Backlog, To Do, Open → not-started; Done, Closed,
Rejected → finished), else the first is not-started, the last finished, the
rest started. The first state is the default for new entities.

Prints the plan; nothing changes without --yes. Add other fields afterwards
with 'fibery schema field add'.

Examples:
  fibery schema db create "Space/Work" --dry-run
  fibery schema db create "Space/Work" --state Backlog --state "In Progress" \
    --state Review=started --state Done --state Rejected=finished --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("no schema — run: fibery schema sync")
		}
		s := dbCreateSpecFlags
		s.DB = args[0]
		s.Workflow, s.Comments, s.Files, s.Description = !dbCreateNoWorkflow, !dbCreateNoCom, !dbCreateNoFiles, !dbCreateNoDesc
		if s.States, err = parseStates(dbCreateStates); err != nil {
			return err
		}
		if len(s.States) > 0 && !s.Workflow {
			return fmt.Errorf("--state needs the workflow (drop --no-workflow)")
		}
		batch, err := dbCreateBatch(s)
		if err != nil {
			return err
		}
		if requireDatabase(schema, s.DB) == nil {
			return fmt.Errorf("database %s already exists", s.DB)
		}
		if _, err := spaceIDByName(cmd, spaceOf(s.DB)); err != nil {
			return err
		}

		fmt.Printf("## Create %s\n\n", s.DB)
		parts := []string{"Name (title)"}
		if s.Description {
			parts = append(parts, "Description (rich text)")
		}
		if s.Files {
			parts = append(parts, "Files")
		}
		if s.Comments {
			parts = append(parts, "comments")
		}
		fmt.Printf("Fields: %s\n", strings.Join(parts, ", "))
		switch {
		case !s.Workflow:
			fmt.Println("Workflow: none")
		case len(s.States) == 0:
			fmt.Println("Workflow: To Do → In Progress → Done (built-in)")
		default:
			var names []string
			for _, st := range s.States {
				names = append(names, fmt.Sprintf("%s (%s)", st.Name, st.Type))
			}
			fmt.Printf("Workflow: %s · default %s\n", strings.Join(names, " → "), s.States[0].Name)
		}
		if dbCreateDryRun {
			fmt.Println("Dry run — nothing changed.")
			return nil
		}
		if !dbCreateYes {
			return fmt.Errorf("re-run with --yes to create the database above")
		}

		if _, err := cli.One(ctx, client.Command{Command: "fibery.schema/batch", Args: map[string]any{"commands": batch}}); err != nil {
			return fmt.Errorf("create %s: %w", s.DB, err)
		}
		fmt.Printf("Created %s.\n", s.DB)
		if len(s.States) > 0 {
			old, err := stateIDs(cmd, s.DB)
			if err != nil {
				return fmt.Errorf("database created, workflow left built-in: %w", err)
			}
			if _, err := cli.One(ctx, stateReplaceCommands(s.DB, s.States, old, client.NewUUID)); err != nil {
				return fmt.Errorf("database created, workflow left built-in: %w", err)
			}
			fmt.Printf("Workflow set: %d states.\n", len(s.States))
		}
		if err := runSchemaSync(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: schema re-sync failed: %v — run 'fibery schema sync'\n", err)
		}
		return nil
	},
}

// stateIDs lists the ids of db's current workflow states.
func stateIDs(cmd *cobra.Command, db string) ([]string, error) {
	res, err := cli.One(cmd.Context(), client.Command{Command: "fibery.entity/query", Args: map[string]any{
		"query": map[string]any{
			"q/from":   "workflow/state_" + db,
			"q/select": map[string]any{"id": "fibery/id"},
			"q/limit":  "q/no-limit",
		},
	}})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no built-in states found for %s", db)
	}
	return ids, nil
}

func init() {
	f := schemaDBCreateCmd.Flags()
	f.StringArrayVar(&dbCreateStates, "state", nil, "workflow state \"Name[=not-started|started|finished]\" (repeatable, in order; replaces To Do/In Progress/Done)")
	f.StringVar(&dbCreateSpecFlags.Color, "color", "", "badge colour, e.g. #4FAF54")
	f.BoolVar(&dbCreateNoWorkflow, "no-workflow", false, "no workflow (state) field")
	f.BoolVar(&dbCreateNoCom, "no-comments", false, "no comments")
	f.BoolVar(&dbCreateNoFiles, "no-files", false, "no Files field")
	f.BoolVar(&dbCreateNoDesc, "no-description", false, "no Description rich-text field")
	f.BoolVar(&dbCreateYes, "yes", false, "create the database (required)")
	f.BoolVar(&dbCreateDryRun, "dry-run", false, "print the plan and stop")
	schemaDBCmd.AddCommand(schemaDBCreateCmd)
}
