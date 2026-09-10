package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/iamnikolie/fibery-cli/internal/render"
	"github.com/spf13/cobra"
)

var meCmd = &cobra.Command{
	Use:   "me",
	Short: "Show current user",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from": "fibery/user",
					"q/select": map[string]any{
						"fibery/id":   "fibery/id",
						"user/email":  "user/email",
						"user/name":   "user/name",
						"fibery/role": "fibery/role",
					},
					"q/where": []any{"=", []any{"fibery/id"}, "$my-id"},
					"q/limit": 1,
				},
			},
		})
		if err != nil {
			return err
		}
		// result is an array — unwrap first element
		var items []json.RawMessage
		if err := json.Unmarshal(result, &items); err != nil || len(items) == 0 {
			return fmt.Errorf("could not read current user")
		}
		return outputJSON(items[0], func() error {
			return render.KV(os.Stdout, items[0])
		})
	},
}

func init() {
	rootCmd.AddCommand(meCmd)
}
