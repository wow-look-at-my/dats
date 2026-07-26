package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

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
// subcommand: it resolves the -j/--jobs flag and runs the given files. It
// passes context.Background() -- plain `dats test` installs no signal
// handling and behaves exactly as before; only `dats watch` passes a
// cancelable context.
func runTestsCommand(cmd *cobra.Command, args []string) error {
	jobs, err := resolveJobs(cmd.Flags())
	if err != nil {
		return err
	}
	sandbox, err := resolveSandbox(cmd.Flags())
	if err != nil {
		return err
	}
	return runTests(context.Background(), args, os.Stdout, jobs, sandbox)
}

// runTests runs every resolved file: serially when jobs is 0 (the historical
// code path, unchanged), or with up to jobs concurrently-running commands
// when jobs >= 1. Both modes report identical totals and exit status.
// Canceling ctx kills in-flight commands (teardown still runs) and the
// aborted instances report as failures. A nil sandbox runs commands directly
// on the host; the CLI passes one unless the run opted out.
func runTests(ctx context.Context, args []string, out io.Writer, jobs int, sandbox *runner.SandboxConfig) error {
	files, err := resolveFiles(args)
	if err != nil {
		return err
	}

	r := runner.NewRunner(out, verbose, keepTemp, coverDir)
	r.Update = updateGoldens
	r.Sandbox = sandbox

	// Wall time of the execution phase, consumed only by the report files;
	// stdout output never mentions it. Hard errors below abort the run
	// without writing reports (today's control flow, unchanged).
	start := time.Now()

	var results []*runner.FileResult
	if jobs > 0 {
		results, err = r.RunFilesParallel(ctx, files, jobs)
		if err != nil {
			// Already carries the "running <path>:" context.
			return err
		}
	} else {
		for _, path := range files {
			result, err := r.RunFile(ctx, path)
			if err != nil {
				return fmt.Errorf("running %s: %w", path, err)
			}
			results = append(results, result)
		}
	}

	wall := time.Since(start)

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

	// Under --update, summarize the golden churn (writes and prunes were
	// already listed per file). Silent when nothing changed.
	if updateGoldens {
		updated, pruned := 0, 0
		for _, result := range results {
			for i := range result.Results {
				updated += len(result.Results[i].UpdatedGoldens)
			}
			pruned += len(result.PrunedGoldens)
		}
		if updated+pruned > 0 {
			fmt.Fprintf(out, "\nUpdated %d golden file(s)", updated)
			if pruned > 0 {
				fmt.Fprintf(out, ", pruned %d stale", pruned)
			}
			fmt.Fprintln(out)
		}
	}

	// Reports are written whenever the run executed -- especially when tests
	// failed and the run is about to exit 1. A report that cannot be written
	// is a real error (stderr message, exit 1) even when every test passed,
	// so it takes precedence over the silent errTestsFailed sentinel.
	if err := writeReports(results, wall); err != nil {
		return err
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
