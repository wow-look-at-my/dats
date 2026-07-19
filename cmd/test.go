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
	RunE: runTestsCommand,
}

// runTestsCommand is the shared RunE of the root command and the test
// subcommand: it resolves the -j/--jobs flag and runs the given files.
func runTestsCommand(cmd *cobra.Command, args []string) error {
	jobs, err := resolveJobs(cmd.Flags())
	if err != nil {
		return err
	}
	return runTests(args, os.Stdout, jobs)
}

// runTests runs every resolved file: serially when jobs is 0 (the historical
// code path, unchanged), or with up to jobs concurrently-running commands
// when jobs >= 1. Both modes report identical totals and exit status.
func runTests(args []string, out io.Writer, jobs int) error {
	files, err := resolveFiles(args)
	if err != nil {
		return err
	}

	r := runner.NewRunner(out, verbose, keepTemp, coverDir)

	var results []*runner.FileResult
	if jobs > 0 {
		results, err = r.RunFilesParallel(files, jobs)
		if err != nil {
			// Already carries the "running <path>:" context.
			return err
		}
	} else {
		for _, path := range files {
			result, err := r.RunFile(path)
			if err != nil {
				return fmt.Errorf("running %s: %w", path, err)
			}
			results = append(results, result)
		}
	}

	totalPassed := 0
	totalFailed := 0
	anyFailed := false

	for _, result := range results {
		totalPassed += result.Passed
		totalFailed += result.Failed
		if !result.Ok() {
			// Covers failing tests and teardown failures, which fail the
			// file even when every test passed.
			anyFailed = true
		}
	}

	if len(files) > 1 {
		fmt.Fprintf(out, "\nTotal: %d/%d passed", totalPassed, totalPassed+totalFailed)
		if totalFailed > 0 {
			fmt.Fprintf(out, ", %d failed", totalFailed)
		}
		fmt.Fprintln(out)
	}

	if anyFailed {
		return errTestsFailed
	}

	return nil
}

func init() {
	// --keep-temp and --coverdir are registered as persistent flags on rootCmd
	// (see root.go) and inherited here.
	rootCmd.AddCommand(testCmd)
}
