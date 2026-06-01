package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/langgerone/fibery-cli/internal/render"
)

// isScalarResult returns true if result is a non-null, non-object, non-array JSON value.
func isScalarResult(result json.RawMessage) bool {
	if result == nil {
		return false
	}
	trimmed := strings.TrimSpace(string(result))
	return trimmed != "null" &&
		!strings.HasPrefix(trimmed, "{") &&
		!strings.HasPrefix(trimmed, "[")
}

// isDestructiveCommand returns true when a fibery command name implies data
// loss. Used to surface a stderr warning so agents don't silently nuke things.
func isDestructiveCommand(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "delete") ||
		strings.Contains(n, "remove") ||
		strings.Contains(n, "drop")
}

var execYes bool

var execCmd = &cobra.Command{
	Use:   "exec <command-json>",
	Short: "Send a raw Fibery command (any mutation or query)",
	Long: `Send any command to POST /api/commands. The JSON must have "command" and "args" keys.
Destructive commands (delete/remove/drop) require --yes.

Examples:
  fibery exec '{"command":"fibery.entity/delete","args":{"type":"Space/Database","entity":{"fibery/id":"<uuid>"}}}' --yes
  fibery exec '{"command":"fibery.entity/create","args":{"type":"Space/Database","entity":{"Space/Name":"new item"}}}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var c client.Command
		if err := json.Unmarshal([]byte(args[0]), &c); err != nil {
			return fmt.Errorf("invalid command JSON: %w", err)
		}
		if isDestructiveCommand(c.Command) && !execYes {
			return fmt.Errorf("command %q is destructive — add --yes to confirm", c.Command)
		}
		result, err := cli.One(cmd.Context(), c)
		if err != nil {
			return err
		}
		return outputJSON(result, func() error {
			if result == nil || string(result) == "null" {
				fmt.Println("OK")
				return nil
			}
			if isScalarResult(result) {
				var s string
				if json.Unmarshal(result, &s) == nil {
					fmt.Println(s)
				} else {
					fmt.Println(strings.TrimSpace(string(result)))
				}
				return nil
			}
			return render.KV(os.Stdout, result)
		})
	},
}

func init() {
	execCmd.Flags().BoolVar(&execYes, "yes", false, "confirm a destructive command (delete/remove/drop)")
	rootCmd.AddCommand(execCmd)
}
