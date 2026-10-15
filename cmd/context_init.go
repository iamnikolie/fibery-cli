package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/profile"
)

var contextInitSave bool

var contextInitCmd = &cobra.Command{
	Use:   "init <db>",
	Short: "Generate a draft context profile for a database (heuristic)",
	Long: `Print a heuristic draft profile (keep / docs / edges) for a database.

Review and edit the draft, then persist it with --save. The runtime command
'fibery context <id>' reads the saved profile; it never re-runs the heuristic.

Example:
  fibery context init "Development/Dev Task"            # print draft to stdout
  fibery context init "Development/Dev Task" --save     # save to ~/.fibery/context.yaml`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := args[0]
		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("context init: %w (run 'fibery schema sync')", err)
		}
		if findTitleField(schema, db) == "" && !schemaHasField(schema, db, "fibery/id") {
			return fmt.Errorf("context init: database %q not found in schema — check the name or run 'fibery schema sync'", db)
		}
		prof, unclassified := classifyProfile(schema, db)

		if contextInitSave {
			p, err := profile.Load(account)
			if err != nil {
				return err
			}
			p.Set(db, prof)
			if err := profile.Save(p, account); err != nil {
				return err
			}
			cmd.PrintErrf("Saved profile for %q.\n", db)
			return nil
		}

		out, err := draftYAML(db, prof, unclassified)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stdout, out)
		return nil
	},
}

// draftYAML renders a single-db profiles document plus a trailing comment block
// listing relation fields the heuristic could not classify, so the agent can
// promote any of them into an edge role before saving.
func draftYAML(db string, prof profile.Profile, unclassified []string) (string, error) {
	doc := profile.Profiles{Profiles: map[string]profile.Profile{db: prof}}
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("draftYAML: %w", err)
	}
	var b strings.Builder
	b.Write(data)
	if len(unclassified) > 0 {
		b.WriteString("\n# unclassified relation fields (promote into edges.* if useful):\n")
		for _, u := range unclassified {
			b.WriteString("#   ")
			b.WriteString(u)
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

func init() {
	contextInitCmd.Flags().BoolVar(&contextInitSave, "save", false, "persist the profile to the account's context.yaml")
	// contextInitCmd is registered on contextCmd in cmd/context.go init().
}
