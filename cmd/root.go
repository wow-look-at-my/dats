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
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")
	// Add test-specific flags to root too so `dats --keep-temp file.dats` works
	rootCmd.Flags().BoolVar(&keepTemp, "keep-temp", false, "Keep temp directory for debugging")
	rootCmd.Flags().StringVar(&coverDir, "coverdir", "", "Set GOCOVERDIR on executed commands to collect coverage data")
}
