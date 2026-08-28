package cmd

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/dats/runner"

	dats "github.com/wow-look-at-my/dats"
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
	sshTarget, err := resolveSSH(cmd.Flags())
	if err != nil {
		return err
	}
	return runTests(context.Background(), args, os.Stdout, jobs, sandbox, sshTarget)
}

// runTests is the CLI's thin layer over dats.Run: it maps the parsed flags
// onto Options, writes the requested report files, and turns a red run into
// the silent errTestsFailed sentinel Execute exits 1 on. Everything else --
// file resolution, execution, the human-readable output and its totals --
// belongs to the library, so a caller that links dats gets byte-identical
// behavior instead of a reimplementation.
//
// A nil sandbox means the run opted out; the library's zero Sandbox is auto,
// so opting out has to be spelled explicitly here.
func runTests(ctx context.Context, args []string, out io.Writer, jobs int, sandbox *runner.SandboxConfig, sshTarget string) error {
	opts := dats.Options{
		Paths:    args,
		Output:   out,
		Jobs:     jobs,
		SSH:      dats.SSH{Target: sshTarget},
		Verbose:  verbose,
		Update:   updateGoldens,
		KeepTemp: keepTemp,
		CoverDir: coverDir,
		Sandbox:  dats.Sandbox{Mode: runner.SandboxNone},
	}
	if sandbox != nil {
		opts.Sandbox = dats.Sandbox{Mode: sandbox.Mode, Image: sandbox.Image}
	}

	result, err := dats.Run(ctx, opts)
	if err != nil {
		return err
	}

	// Reports are written whenever the run executed -- especially when tests
	// failed and the run is about to exit 1. A report that cannot be written
	// is a real error (stderr message, exit 1) even when every test passed,
	// so it takes precedence over the silent errTestsFailed sentinel.
	if err := writeReports(result.Files, result.Wall); err != nil {
		return err
	}

	if !result.Ok() {
		return errTestsFailed
	}

	return nil
}

func init() {
	// --keep-temp and --coverdir are registered as persistent flags on rootCmd
	// (see root.go) and inherited here.
	rootCmd.AddCommand(testCmd)
}
