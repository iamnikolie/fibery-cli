package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, injected at link time by the Makefile and by GoReleaser:
//
//	-ldflags "-X github.com/iamnikolie/fibery-cli/cmd.version=v1.0.0"
//
// A plain `go build` or `go install` leaves the defaults, so an unstamped
// binary reports itself as a dev build rather than lying about a version.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionString is the single line printed by `fibery --version` and
// `fibery version`. Go version and platform are included because the most
// common bug report is "works on my machine".
func versionString() string {
	return fmt.Sprintf("fibery %s (commit %s, built %s, %s %s/%s)",
		version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// cmd.Println writes to stderr in cobra, which makes
		// `v=$(fibery version)` come back empty. Version is data — stdout.
		fmt.Fprintln(cmd.OutOrStdout(), versionString())
		return nil
	},
}

func init() {
	// Setting rootCmd.Version is what makes cobra wire up the --version flag.
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
