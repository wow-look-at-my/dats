package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "dats [files-or-dirs...]",
	Short: "Declarative Automated Testing System",
	Long: `DATS runs tests defined in declarative YAML files (.dats).

A test is a command. dats runs it with bash, captures the exit code, stdout,
stderr and the files it wrote, and checks them against the file's assertions.
.dats files are indented with TABS -- spaces are a parse error:

	tests:
		- desc: greets the world
		  cmd: echo hello
		  outputs:
			stdout:
				- hello

Arguments are .dats files or directories in any mix; with no arguments dats
discovers every .dats file under the working directory. Test commands are
SANDBOXED by default (bubblewrap, then seatbelt, then docker): a command may
only write inside its own temp directory, and --no-sandbox is the only opt-out
-- a .dats file can narrow its own sandbox but never switch it off.

The exit status is 0 when every test passed, and 1 when anything failed or the
run could not be carried out at all.

This binary carries its own complete documentation:

	dats docs             list the documentation topics
	dats docs format      every .dats key, placeholder and assertion
	dats docs cli         flags, discovery, sandboxing, -j, watch mode, output
	dats docs examples    annotated .dats files to copy from
	dats docs all         everything, in one stream`,
	Example: `  dats                            # run every .dats file in the tree
  dats tests/ smoke.dats          # run a directory and a file
  dats -v --keep-temp one.dats    # verbose, and keep the temp dir to inspect
  dats --no-sandbox one.dats      # run the commands straight on the host
  dats watch tests/               # re-run on every change until Ctrl-C
  dats syntax tests/              # parse and validate, run nothing`,
	RunE:          runTestsCommand,
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and exits with a failure status when it fails.
func Execute() {
	// Make-style -jN needs rewriting to --jobs=N before cobra parses the args; see normalizeJobsShorthand.
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
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show command details, durations, and full output on failure")
	rootCmd.PersistentFlags().BoolVar(&keepTemp, "keep-temp", false, "Keep the per-run temp directory (prints its path) for debugging")
	rootCmd.PersistentFlags().StringVar(&coverDir, "coverdir", "", "Set GOCOVERDIR on every executed command (tests and hooks) to collect coverage data")
	registerJobsFlag(rootCmd.PersistentFlags())
	registerSandboxFlags(rootCmd.PersistentFlags())
	registerSSHFlag(rootCmd.PersistentFlags())
	registerReportFlags(rootCmd.PersistentFlags())
	registerUpdateFlag(rootCmd.PersistentFlags())
}
