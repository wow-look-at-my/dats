package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/dats/runner"
)

var (
	keepTemp bool
	coverDir string
)

// errTestsFailed signals that at least one test failed. The runner output has
// already reported the failures, so Execute exits 1 without printing more.
var errTestsFailed = errors.New("tests failed")

var testCmd = &cobra.Command{
	Use:   "test [files...]",
	Short: "Run tests from .dats files",
	Long: `Run tests defined in .dats files. If no files are specified,
recursively finds and runs all .dats files in the current directory tree.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTests(args, os.Stdout)
	},
}

func runTests(args []string, out io.Writer) error {
	files, err := resolveFiles(args)
	if err != nil {
		return err
	}

	r := runner.NewRunner(out, verbose, keepTemp, coverDir)

	totalPassed := 0
	totalFailed := 0

	for _, path := range files {
		result, err := r.RunFile(path)
		if err != nil {
			return fmt.Errorf("running %s: %w", path, err)
		}
		totalPassed += result.Passed
		totalFailed += result.Failed
	}

	if len(files) > 1 {
		fmt.Fprintf(out, "\nTotal: %d/%d passed", totalPassed, totalPassed+totalFailed)
		if totalFailed > 0 {
			fmt.Fprintf(out, ", %d failed", totalFailed)
		}
		fmt.Fprintln(out)
	}

	if totalFailed > 0 {
		return errTestsFailed
	}

	return nil
}

func init() {
	// --keep-temp and --coverdir are registered as persistent flags on rootCmd
	// (see root.go) and inherited here.
	rootCmd.AddCommand(testCmd)
}
