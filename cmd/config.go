package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage fibery-cli configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Set API token and workspace (writes ~/.fibery/config.yaml)",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := bufio.NewReader(os.Stdin)

		fmt.Print("Fibery workspace (e.g. acme for acme.fibery.io): ")
		ws, _ := r.ReadString('\n')
		ws = strings.TrimSpace(ws)

		fmt.Print("API token: ")
		tok, _ := r.ReadString('\n')
		tok = strings.TrimSpace(tok)

		if err := config.Save(tok, ws, account); err != nil {
			return err
		}
		if account == "" {
			fmt.Println("Saved to ~/.fibery/config.yaml")
		} else {
			fmt.Printf("Saved to ~/.fibery/%s/config.yaml\n", account)
		}
		return nil
	},
}

func init() {
	configCmd.AddCommand(configInitCmd)
	rootCmd.AddCommand(configCmd)
}
