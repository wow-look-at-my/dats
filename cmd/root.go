package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "dats",
	Short: "Declarative Automated Testing System",
	Long:  "DATS runs tests defined in declarative YAML files (.dats).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTests(args)
	},
	Args: cobra.ArbitraryArgs,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Persistent flags are inherited by subcommands, so `dats --keep-temp f.dats`
	// and `dats test --keep-temp f.dats` both work with a single registration.
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")
	rootCmd.PersistentFlags().BoolVar(&keepTemp, "keep-temp", false, "Keep temp directory for debugging")
	rootCmd.PersistentFlags().StringVar(&coverDir, "coverdir", "", "Set GOCOVERDIR on executed commands to collect coverage data")
}
