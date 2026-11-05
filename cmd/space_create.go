package cmd

import (
	"fmt"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	spaceCreateColor, spaceCreateIcon string
	spaceCreateYes, spaceCreateDryRun bool
)

// spaceCreateCommands saves an empty space and installs it — the two steps the
// UI's "New space" takes. fibery.app/install alone answers app-not-found, and
// fibery/app entities cannot be created with fibery.entity/create.
func spaceCreateCommands(name, color, icon string) []client.Command {
	app := map[string]any{
		"fibery/name":          name,
		"fibery/namespace":     name,
		"fibery/title":         name,
		"fibery/version":       "1.0.0",
		"fibery/schema":        map[string]any{"fibery/name": name, "fibery/types": []any{}},
		"fibery/color":         color,
		"fibery/show-in-menu?": true,
	}
	if icon != "" {
		app["fibery/icon"] = icon
	}
	return []client.Command{
		{Command: "fibery.app/save", Args: map[string]any{"app": app}},
		{Command: "fibery.app/install", Args: map[string]any{"name": name, "version": "1.0.0"}},
	}
}

// validSpaceName rejects names Fibery cannot use as a type namespace.
func validSpaceName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("space name is empty")
	case strings.ContainsAny(name, "/_~"):
		return fmt.Errorf("space name %q must not contain '/', '_' or '~'", name)
	}
	return nil
}

var spaceCreateCmd = &cobra.Command{
	Use:   "create <space>",
	Short: "Create an empty space",
	Long: `Create an empty space (what "New space" in the UI makes), ready for
'fibery schema db create "<space>/<Database>"' and wiki documents.
Nothing changes without --yes.

Example:
  fibery space create Development --dry-run
  fibery space create Development --color "#199EE3" --icon folder --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := validSpaceName(name); err != nil {
			return err
		}
		apps, err := loadUserSpaces(cmd)
		if err != nil {
			return err
		}
		for _, n := range apps {
			if strings.EqualFold(n, name) {
				return fmt.Errorf("space %q already exists", n)
			}
		}
		fmt.Printf("## Create space %s\n\n", name)
		if spaceCreateDryRun {
			fmt.Println("Dry run — nothing changed.")
			return nil
		}
		if !spaceCreateYes {
			return fmt.Errorf("re-run with --yes to create the space")
		}
		for _, c := range spaceCreateCommands(name, spaceCreateColor, spaceCreateIcon) {
			if _, err := cli.One(cmd.Context(), c); err != nil {
				return fmt.Errorf("%s: %w", c.Command, err)
			}
		}
		fmt.Printf("Created space %s.\n", name)
		resync(cmd)
		return nil
	},
}

func init() {
	f := spaceCreateCmd.Flags()
	f.StringVar(&spaceCreateColor, "color", "#199EE3", "space colour")
	f.StringVar(&spaceCreateIcon, "icon", "", "icon name, e.g. folder")
	f.BoolVar(&spaceCreateYes, "yes", false, "create (required)")
	f.BoolVar(&spaceCreateDryRun, "dry-run", false, "print the plan and stop")
	spaceCmd.AddCommand(spaceCreateCmd)
}
