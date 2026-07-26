package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "dats",
	Short: "Declarative Automated Testing System",
	Long:  "DATS runs tests defined in declarative YAML files (.dats).",
	RunE:  runTestsCommand,
	Args:  cobra.ArbitraryArgs,
	// Errors are reported by Execute (or, for test/syntax failures, already
	// reported by the runner output); cobra should not add usage dumps or a
	// second error line.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits non-zero on failure. Sentinel
// failures (failing tests or syntax checks) have already been reported by
// their command's output, so they exit silently; any other error is printed
// exactly once.
func Execute() {
	// Make-style -jN needs rewriting to --jobs=N before cobra parses the
	// args; see normalizeJobsShorthand.
	rootCmd.SetArgs(normalizeJobsShorthand(os.Args[1:]))
	err := rootCmd.Execute()
	if err == nil {
		return
	}
	if !errors.Is(err, errTestsFailed) && !errors.Is(err, errSyntaxFailed) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(1)
}

func init() {
	// Persistent flags are inherited by subcommands, so `dats --keep-temp f.dats`
	// and `dats test --keep-temp f.dats` both work with a single registration.
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")
	rootCmd.PersistentFlags().BoolVar(&keepTemp, "keep-temp", false, "Keep temp directory for debugging")
	rootCmd.PersistentFlags().StringVar(&coverDir, "coverdir", "", "Set GOCOVERDIR on executed commands to collect coverage data")
	registerJobsFlag(rootCmd.PersistentFlags())
	registerSandboxFlags(rootCmd.PersistentFlags())
	registerReportFlags(rootCmd.PersistentFlags())
	registerUpdateFlag(rootCmd.PersistentFlags())
}
