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
}

func Execute() {
	// If no subcommand is given, default to "test"
	if len(os.Args) > 1 {
		// Check if the first arg is a known subcommand or flag
		first := os.Args[1]
		isSubcommand := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == first || cmd.HasAlias(first) {
				isSubcommand = true
				break
			}
		}
		if first == "-h" || first == "--help" || first == "help" || first == "completion" {
			isSubcommand = true
		}
		if !isSubcommand {
			// Inject "test" as the subcommand
			newArgs := make([]string, 0, len(os.Args)+1)
			newArgs = append(newArgs, os.Args[0], "test")
			newArgs = append(newArgs, os.Args[1:]...)
			os.Args = newArgs
		}
	} else {
		// No args at all — default to "test" (which will find .dats files recursively)
		os.Args = append(os.Args, "test")
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")
}
