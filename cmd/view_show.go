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

var viewShowCmd = &cobra.Command{
	Use:   "show <uuid|public-id>",
	Short: "Show a view: type, place, databases, board axes, filter",
	Long: `Describe a view with names instead of ids: where it lives (space or owning
entity), the databases it reads, a board's column and row fields, and whether it
has a filter. --json prints the raw view record (fibery/meta included).

Example:
  fibery view show 306
  fibery view show 306 --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := findView(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if outputFormat == "json" || jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"id": v.ID, "publicId": v.PublicID, "name": v.Name, "type": v.Type,
				"containerType": v.ContainerType, "containerApp": v.ContainerAppID,
				"containerEntity": v.ContainerEntityID, "meta": v.Meta,
			})
		}
		schema, _ := cache.LoadSchema(account)
		place := v.ContainerType
		if v.ContainerType == "app" {
			if apps, err := loadAppNames(cmd.Context()); err == nil {
				place = "space " + apps[v.ContainerAppID]
			}
		} else if v.ContainerType == "object" {
			place = fmt.Sprintf("inside %s #%s", typeNameByID(schema, v.ContainerEntityTypeID), v.ContainerEntityID)
		}
		names, dead := viewDBNames(schema, v.Meta)
		fmt.Printf("## %s\n\n- Type: %s\n- ID: #%s (%s)\n- Place: %s\n- Databases: %s\n", v.Name, v.Type, v.PublicID, v.ID, place, strings.Join(names, ", "))
		if dead {
			fmt.Println("- Dead: every database it reads was deleted")
		}
		var m viewMeta
		if json.Unmarshal(v.Meta, &m) == nil && len(m.Items) > 0 {
			it := m.Items[0]
			if len(it.XExpression) > 0 {
				fmt.Printf("- Columns: %s\n", fieldPathNames(schema, it.XExpression))
			}
			if len(it.YExpression) > 0 {
				fmt.Printf("- Rows: %s\n", fieldPathNames(schema, it.YExpression))
			}
			if it.Query.Where != nil {
				fmt.Println("- Filter: yes")
			}
		}
		return nil
	},
}

func fieldPathNames(schema map[string]any, ids []string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		if n := fieldNameByID(schema, id); n != "" {
			out[i] = n
		} else {
			out[i] = id
		}
	}
	return strings.Join(out, " → ")
}

var viewRenameCmd = &cobra.Command{
	Use:   "rename <uuid|public-id> <new name>",
	Short: "Rename a view",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := findView(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if err := cli.UpdateViews(cmd.Context(), []map[string]any{{"id": v.ID, "values": map[string]any{"fibery/name": args[1]}}}); err != nil {
			return err
		}
		got, err := findView(cmd.Context(), v.ID)
		if err != nil || got.Name != args[1] {
			return fmt.Errorf("rename sent but the view still reads %q", nameOr(got))
		}
		fmt.Printf("Renamed %s view #%s: %q → %q.\n", v.Type, v.PublicID, v.Name, got.Name)
		return nil
	},
}

func nameOr(v *client.ViewRecord) string {
	if v == nil {
		return "?"
	}
	return v.Name
}

func init() {
	viewCmd.AddCommand(viewShowCmd, viewRenameCmd)
}
