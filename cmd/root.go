package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
	"github.com/langgerone/fibery-cli/internal/config"
	"github.com/langgerone/fibery-cli/internal/render"
)

var (
	jsonOutput   bool
	account      string
	verbose      bool
	outputFormat string
	cfg          *config.Config
	cli          *client.Client
)

var rootCmd = &cobra.Command{
	Use:   "fibery",
	Short: "Fibery CLI — workspace at your fingertips",
	Long: `Fibery CLI — workspace at your fingertips.

Run 'fibery skill' to print the full Claude skill reference (commands, flags, workflows).`,
	// On RunE errors cobra prints the error itself — usage is noise for both
	// humans and agents, and the error messages already include the recovery hint.
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// config init doesn't need auth
		if cmd.Name() == "init" {
			return nil
		}
		var err error
		cfg, err = config.Load(account)
		if err != nil {
			return err
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		cli = client.New(cfg.APIToken, cfg.BaseURL())
		cli.Verbose = verbose
		if !cache.HasSchema(account) {
			cmd.PrintErrln("No schema cache found — fetching schema…")
			return runSchemaSync(cmd.Context())
		}
		if mt, err := cache.SchemaModTime(account); err == nil {
			if warn := schemaAgeWarning(mt, schemaStaleAfter); warn != "" {
				cmd.PrintErrln(warn)
			}
		}
		return nil
	},
}

// schemaStaleAfter is how old the cache may be before users see a warning.
const schemaStaleAfter = 7 * 24 * time.Hour

// schemaAgeWarning returns a stderr-bound warning string when the cache mtime
// is older than threshold. Empty when fresh.
func schemaAgeWarning(modTime time.Time, threshold time.Duration) string {
	age := time.Since(modTime)
	if age <= threshold {
		return ""
	}
	return fmt.Sprintf("Warning: schema cache is %d days old — run 'fibery schema sync' to refresh.", int(age.Hours()/24))
}

func Execute() {
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output raw JSON")
	rootCmd.PersistentFlags().StringVar(&account, "config", os.Getenv("FIBERY_CONFIG"), "account config to use (subdirectory of ~/.fibery/)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "print API request JSON to stderr")
	rootCmd.PersistentFlags().StringVar(&outputFormat, "format", "", "output format: table (default), json, csv, tsv")
}

// outputJSON prints raw JSON if --json flag set, otherwise calls renderFn.
func outputJSON(data json.RawMessage, renderFn func() error) error {
	switch outputFormat {
	case "json":
		os.Stdout.Write(data)
		os.Stdout.Write([]byte("\n"))
		return nil
	case "csv":
		return render.CSV(os.Stdout, data)
	case "tsv":
		return render.TSV(os.Stdout, data)
	default:
		if jsonOutput {
			os.Stdout.Write(data)
			os.Stdout.Write([]byte("\n"))
			return nil
		}
		return renderFn()
	}
}
