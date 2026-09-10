package cmd

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed skill.md
var skillDoc string

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Print the Claude skill reference for this CLI",
	Long:  "Prints the full fibery-cli skill document for use as Claude context.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print(skillDoc)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(skillCmd)
}
